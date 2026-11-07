package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"seccopy/internal/indexpb"
	"seccopy/internal/repopb"
	"seccopy/internal/rpcmeta"
	"seccopy/internal/store"
	"seccopy/internal/tree"
)

const (
	chunkSize      = 256 * 1024
	defaultMaxRead = 256
)

type PeerCloser interface {
	ClosePeer(ctx context.Context)
}

type Server struct {
	repopb.UnimplementedRepoServer
	stores   map[string]*store.Store
	reads    chan struct{}
	authUser string
	authPass string
	peers    PeerCloser
}

func New(st *store.Store, maxReads int, user, pass string, peers PeerCloser) *Server {
	if maxReads <= 0 {
		maxReads = defaultMaxRead
	}
	return &Server{
		stores:   map[string]*store.Store{st.Name: st},
		reads:    make(chan struct{}, maxReads),
		authUser: user,
		authPass: pass,
		peers:    peers,
	}
}

func (s *Server) GetIndex(ctx context.Context, req *repopb.GetIndexRequest) (*repopb.GetIndexResponse, error) {
	st, err := s.lookupRepo(req.GetRepo())
	if err != nil {
		return nil, err
	}
	return &repopb.GetIndexResponse{Index: st.Index, Signature: st.Sig}, nil
}

func (s *Server) GetFile(req *repopb.GetFileRequest, stream grpc.ServerStreamingServer[repopb.FileChunk]) error {
	ctx := stream.Context()
	st, err := s.lookupRepo(req.GetRepo())
	if err != nil {
		return err
	}

	rel := req.GetPath()
	full, err := tree.ResolveInRoot(st.Root, rel)
	if err != nil {
		s.reject(ctx, "path_traversal", rel, err)
		return status.Error(codes.PermissionDenied, "invalid path")
	}

	entry, ok := st.Offered[rel]
	if !ok {
		return status.Error(codes.NotFound, "not found")
	}
	if entry.GetKind() != indexpb.EntryKind_ENTRY_KIND_FILE {
		return status.Error(codes.FailedPrecondition, "not a regular file")
	}

	select {
	case s.reads <- struct{}{}:
		defer func() { <-s.reads }()
	case <-ctx.Done():
		return ctx.Err()
	}

	f, err := tree.OpenRegular(full)
	if err != nil {
		return status.Error(codes.NotFound, "not found")
	}
	defer f.Close()

	bufp := chunkPool.Get().(*[]byte)
	defer chunkPool.Put(bufp)
	buf := *bufp

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&repopb.FileChunk{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "read: %v", err)
		}
	}
}

func (s *Server) lookupRepo(name string) (*store.Store, error) {
	if name == "" {
		name = "default"
	}
	st, ok := s.stores[name]
	if !ok {
		return nil, status.Error(codes.NotFound, "unknown repository")
	}
	return st, nil
}

func (s *Server) reject(ctx context.Context, event, path string, err error) {
	slog.Error("security", "event", event, "path", path, "err", err)
	if s.peers != nil {
		s.peers.ClosePeer(ctx)
	}
}

func (s *Server) UnaryAuth(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := s.checkAuth(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (s *Server) StreamAuth(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := s.checkAuth(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}

func (s *Server) checkAuth(ctx context.Context) error {
	if s.authUser == "" {
		return nil
	}
	md, _ := metadata.FromIncomingContext(ctx)
	user := firstMD(md, rpcmeta.User)
	pass := firstMD(md, rpcmeta.Password)
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.authUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.authPass)) == 1
	if userOK && passOK {
		return nil
	}
	return status.Error(codes.Unauthenticated, "unauthenticated")
}

func firstMD(md metadata.MD, key string) string {
	vals := md.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

var chunkPool = sync.Pool{
	New: func() any {
		b := make([]byte, chunkSize)
		return &b
	},
}
