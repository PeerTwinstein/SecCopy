package main

import (
	"flag"
	"fmt"
	"os"

	"seccopy/internal/manifest"
	"seccopy/internal/sign"
)

func main() {
	repo := flag.String("repo", "default", "repository name")
	gpgUser := flag.String("gpg-user", "", "GPG key id or fingerprint (APT mirror key)")
	gpgBin := flag.String("gpg", "gpg", "gpg binary")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: seccopy-publish [flags] <directory>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if *gpgUser == "" {
		fmt.Fprintln(os.Stderr, "seccopy-publish: -gpg-user is required")
		os.Exit(2)
	}

	root := flag.Arg(0)
	n, err := manifest.Publish(root, *repo, sign.GPG{Bin: *gpgBin, User: *gpgUser})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seccopy-publish: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "published %d entries to %s (repo %s)\n", n, root, *repo)
}
