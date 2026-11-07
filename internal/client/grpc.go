package client

import (
	"context"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"seccopy/internal/repopb"
	"seccopy/internal/rpcmeta"
)

type GRPC struct {
	API repopb.RepoClient
}

func (g GRPC) GetIndex(ctx context.Context, repo string) ([]byte, []byte, error) {
	resp, err := g.API.GetIndex(ctx, &repopb.GetIndexRequest{Repo: repo})
	if err != nil {
		return nil, nil, err
	}
	return resp.GetIndex(), resp.GetSignature(), nil
}

func (g GRPC) GetFile(ctx context.Context, repo, path string) (io.ReadCloser, error) {
	stream, err := g.API.GetFile(ctx, &repopb.GetFileRequest{Repo: repo, Path: path})
	if err != nil {
		return nil, err
	}
	return &chunkReader{stream: stream}, nil
}

type chunkReader struct {
	stream grpc.ServerStreamingClient[repopb.FileChunk]
	buf    []byte
	err    error
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	for len(r.buf) == 0 {
		chunk, err := r.stream.Recv()
		if err != nil {
			r.err = err
			return 0, err
		}
		r.buf = chunk.GetData()
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *chunkReader) Close() error {
	return r.stream.CloseSend()
}

func AuthUnary(user, pass string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(withAuth(ctx, user, pass), method, req, reply, cc, opts...)
	}
}

func AuthStream(user, pass string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(withAuth(ctx, user, pass), desc, cc, method, opts...)
	}
}

func withAuth(ctx context.Context, user, pass string) context.Context {
	if user == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, rpcmeta.User, user, rpcmeta.Password, pass)
}
