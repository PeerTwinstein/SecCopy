package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"seccopy/internal/client"
	"seccopy/internal/repopb"
	"seccopy/internal/sign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "list":
		os.Exit(runList(args))
	case "get":
		os.Exit(runGet(args))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: seccopy-client list [flags] [prefix]\n")
	fmt.Fprintf(os.Stderr, "       seccopy-client get  [flags] [prefix]\n")
}

func commonFlags(fs *flag.FlagSet, addr, repo, gpgUser, gpgBin, user, pass *string) {
	fs.StringVar(addr, "addr", "127.0.0.1:50051", "server address")
	fs.StringVar(repo, "repo", "default", "repository name")
	fs.StringVar(gpgUser, "gpg-user", "", "expected GPG signer (APT mirror key id)")
	fs.StringVar(gpgBin, "gpg", "gpg", "gpg binary")
	fs.StringVar(user, "user", "", "optional username")
	fs.StringVar(pass, "password", "", "optional password")
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var addr, repo, gpgUser, gpgBin, user, pass string
	commonFlags(fs, &addr, &repo, &gpgUser, &gpgBin, &user, &pass)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if gpgUser == "" {
		fmt.Fprintln(os.Stderr, "seccopy-client: -gpg-user is required")
		return 2
	}
	prefix := fs.Arg(0)
	cfg, err := dial(addr, repo, gpgUser, gpgBin, user, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seccopy-client: %v\n", err)
		return 1
	}
	if err := client.List(context.Background(), os.Stdout, cfg, prefix); err != nil {
		fmt.Fprintf(os.Stderr, "seccopy-client: %v\n", err)
		return 1
	}
	return 0
}

func runGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var addr, repo, gpgUser, gpgBin, user, pass, dest string
	commonFlags(fs, &addr, &repo, &gpgUser, &gpgBin, &user, &pass)
	fs.StringVar(&dest, "dest", "", "destination directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if gpgUser == "" || dest == "" {
		fmt.Fprintln(os.Stderr, "seccopy-client: -gpg-user and -dest are required")
		return 2
	}
	prefix := fs.Arg(0)
	cfg, err := dial(addr, repo, gpgUser, gpgBin, user, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seccopy-client: %v\n", err)
		return 1
	}
	n, err := client.Get(context.Background(), cfg, dest, prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seccopy-client: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "fetched %d entries into %s\n", n, dest)
	return 0
}

func dial(addr, repo, gpgUser, gpgBin, user, pass string) (client.Config, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(client.AuthUnary(user, pass)),
		grpc.WithChainStreamInterceptor(client.AuthStream(user, pass)),
	)
	if err != nil {
		return client.Config{}, err
	}
	return client.Config{
		Fetcher:  client.GRPC{API: repopb.NewRepoClient(conn)},
		Verifier: sign.GPG{Bin: gpgBin, User: gpgUser},
		Repo:     repo,
	}, nil
}
