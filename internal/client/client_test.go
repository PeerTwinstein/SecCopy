package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"seccopy/internal/manifest"
	"seccopy/internal/repopb"
	"seccopy/internal/server"
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
		return fmt.Errorf("bad signature")
	}
	return nil
}

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

func testConfig(t *testing.T, st *store.Store) Config {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	impl := server.New(st, 8, "", "", nil)
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
	return Config{
		Fetcher:  GRPC{API: repopb.NewRepoClient(conn)},
		Verifier: fakeVerifier{},
		Repo:     "default",
	}
}

func TestListAndGet(t *testing.T) {
	st := publishStore(t, map[string][]byte{
		"a.txt":          []byte("hello"),
		"sub/b.txt":      []byte("world"),
		"sub/deep/c.bin": []byte{1, 2, 3},
	})
	cfg := testConfig(t, st)

	var buf bytes.Buffer
	if err := List(context.Background(), &buf, cfg, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "sub/b.txt") {
		t.Fatalf("list output: %s", out)
	}

	dest := t.TempDir()
	n, err := Get(context.Background(), cfg, dest, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "deep", "c.bin")); err != nil {
		t.Fatal(err)
	}
}

func TestGetPrefixOnly(t *testing.T) {
	st := publishStore(t, map[string][]byte{
		"a.txt":     []byte("no"),
		"sub/b.txt": []byte("yes"),
	})
	cfg := testConfig(t, st)
	dest := t.TempDir()
	n, err := Get(context.Background(), cfg, dest, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a.txt must not be fetched")
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "yes" {
		t.Fatalf("got %q", got)
	}
}

func TestGetWipesOnHashMismatch(t *testing.T) {
	st := publishStore(t, map[string][]byte{
		"one.txt": []byte("first"),
		"two.txt": []byte("second"),
	})
	cfg := testConfig(t, st)
	cfg.Fetcher = corruptFile{inner: cfg.Fetcher, path: "two.txt"}
	dest := t.TempDir()
	_, err := Get(context.Background(), cfg, dest, "")
	if err == nil {
		t.Fatal("expected hash mismatch")
	}
	if _, err := os.Stat(filepath.Join(dest, "one.txt")); !os.IsNotExist(err) {
		t.Fatal("first file must be wiped after later failure")
	}
	if _, err := os.Stat(filepath.Join(dest, "two.txt")); !os.IsNotExist(err) {
		t.Fatal("failed file must not remain")
	}
}

func TestListDoesNotWriteFiles(t *testing.T) {
	st := publishStore(t, map[string][]byte{"a.txt": []byte("hello")})
	cfg := testConfig(t, st)
	dest := t.TempDir()
	var buf bytes.Buffer
	if err := List(context.Background(), &buf, cfg, ""); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("list wrote files: %v", entries)
	}
}

func TestGetSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	if _, err := manifest.Publish(root, "default", fakeSigner{}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load(root, "default", fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, st)
	dest := t.TempDir()
	if _, err := Get(context.Background(), cfg, dest, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "target.txt" {
		t.Fatalf("link target %q", got)
	}
}

func TestWipePathZerosAndDeletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wipePath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file still exists")
	}
}

type corruptFile struct {
	inner Fetcher
	path  string
}

func (c corruptFile) GetIndex(ctx context.Context, repo string) ([]byte, []byte, error) {
	return c.inner.GetIndex(ctx, repo)
}

func (c corruptFile) GetFile(ctx context.Context, repo, path string) (io.ReadCloser, error) {
	r, err := c.inner.GetFile(ctx, repo, path)
	if err != nil {
		return nil, err
	}
	if path != c.path {
		return r, nil
	}
	_ = r.Close()
	return io.NopCloser(bytes.NewReader([]byte("tampered-payload"))), nil
}
