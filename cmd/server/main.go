package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"seccopy/internal/repopb"
	"seccopy/internal/server"
	"seccopy/internal/sign"
	"seccopy/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	listen := flag.String("listen", ":50051", "listen address")
	repo := flag.String("repo", "default", "repository name")
	gpgUser := flag.String("gpg-user", "", "expected GPG signer (APT mirror key id)")
	gpgBin := flag.String("gpg", "gpg", "gpg binary")
	user := flag.String("user", "", "optional username")
	password := flag.String("password", "", "optional password")
	maxReads := flag.Int("max-reads", 256, "max concurrent file reads")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: seccopy-server [flags] <data-root>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if *gpgUser == "" {
		fmt.Fprintln(os.Stderr, "seccopy-server: -gpg-user is required")
		os.Exit(2)
	}

	root := flag.Arg(0)
	st, err := store.Load(root, *repo, sign.GPG{Bin: *gpgBin, User: *gpgUser})
	if err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
	slog.Info("repository ready",
		"repo", st.Name,
		"root", st.Root,
		"offered", len(st.Offered),
		"skipped", st.Skipped,
	)

	tracker := &server.ConnTracker{}
	srvImpl := server.New(st, *maxReads, *user, *password, tracker)

	gs := grpc.NewServer(
		grpc.MaxConcurrentStreams(1024),
		grpc.ReadBufferSize(32*1024),
		grpc.WriteBufferSize(32*1024),
		grpc.ChainUnaryInterceptor(srvImpl.UnaryAuth),
		grpc.ChainStreamInterceptor(srvImpl.StreamAuth),
		grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
			return status.Error(codes.Unimplemented, "unknown service")
		}),
	)
	repopb.RegisterRepoServer(gs, srvImpl)

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	}
	lis = tracker.Wrap(lis)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		slog.Info("shutting down")
		gs.GracefulStop()
	}()

	slog.Info("listening", "addr", lis.Addr().String())
	if err := gs.Serve(lis); err != nil {
		slog.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
