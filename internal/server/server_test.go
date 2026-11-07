package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"seccopy/internal/manifest"
	"seccopy/internal/repopb"
	"seccopy/internal/rpcmeta"
	"seccopy/internal/store"
)

type fakeSigner struct{}

func (fakeSigner) Sign(data []byte) ([]byte, error) {
	out := make([]byte, len(data)+4)
	copy(out, []byte("SIG:"))
	copy(out[4:], data)
	return out, nil
}

type fakeVerifier struct{}

func (fakeVerifier) Verify(index, sig []byte) error {
	if !bytes.HasPrefix(sig, []byte("SIG:")) || !bytes.Equal(sig[4:], index) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type recordCloser struct{ n atomic.Int32 }

func (r *recordCloser) ClosePeer(context.Context) { r.n.Add(1) }

func publishStore(t *testing.T, files map[string][]byte) *store.Store {
	t.Helper()
	root := t.TempDir()
	for p, data := range files {
		path := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manifest.Publish(root, "default", fakeSigner{}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load(root, "default", fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func startClient(t *testing.T, st *store.Store, user, pass string, peers PeerCloser) repopb.RepoClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	impl := New(st, 8, user, pass, peers)
	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(impl.UnaryAuth),
		grpc.ChainStreamInterceptor(impl.StreamAuth),
	)
	repopb.RegisterRepoServer(gs, impl)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.Stop(); lis.Close() })

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return repopb.NewRepoClient(conn)
}

func TestGetIndexReturnsSignedBytes(t *testing.T) {
	st := publishStore(t, map[string][]byte{"a.txt": []byte("hello")})
	c := startClient(t, st, "", "", nil)
	got, err := c.GetIndex(context.Background(), &repopb.GetIndexRequest{Repo: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.GetIndex(), st.Index) || !bytes.Equal(got.GetSignature(), st.Sig) {
		t.Fatal("GetIndex must return the exact published bytes")
	}
}

func TestGetFileStreamsContent(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), chunkSize+1024)
	st := publishStore(t, map[string][]byte{"big.bin": payload})
	c := startClient(t, st, "", "", nil)
	stream, err := c.GetFile(context.Background(), &repopb.GetFileRequest{Repo: "default", Path: "big.bin"})
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk.GetData()...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %d bytes, want %d", len(got), len(payload))
	}
}

func TestGetFileRejectsTraversalAndClosesPeer(t *testing.T) {
	st := publishStore(t, map[string][]byte{"a.txt": []byte("a")})
	closer := &recordCloser{}
	c := startClient(t, st, "", "", closer)
	stream, err := c.GetFile(context.Background(), &repopb.GetFileRequest{Repo: "default", Path: "../secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v err=%v", status.Code(err), err)
	}
	if closer.n.Load() != 1 {
		t.Fatalf("ClosePeer calls=%d", closer.n.Load())
	}
}

func TestAuthRequired(t *testing.T) {
	st := publishStore(t, map[string][]byte{"a.txt": []byte("a")})
	c := startClient(t, st, "admin", "secret", nil)
	_, err := c.GetIndex(context.Background(), &repopb.GetIndexRequest{Repo: "default"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%v", status.Code(err))
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		rpcmeta.User, "admin",
		rpcmeta.Password, "secret",
	))
	if _, err := c.GetIndex(ctx, &repopb.GetIndexRequest{Repo: "default"}); err != nil {
		t.Fatal(err)
	}
}
