package client

import (
	"context"
	"fmt"
	"io"

	"seccopy/internal/indexpb"
	"seccopy/internal/sign"
)

type Fetcher interface {
	GetIndex(ctx context.Context, repo string) (index, signature []byte, err error)
	GetFile(ctx context.Context, repo, path string) (io.ReadCloser, error)
}

type Config struct {
	Fetcher  Fetcher
	Verifier sign.Verifier
	Repo     string
}

func (c Config) repo() string {
	if c.Repo == "" {
		return "default"
	}
	return c.Repo
}

func fetchIndex(ctx context.Context, cfg Config) (*indexpb.Index, error) {
	raw, sig, err := cfg.Fetcher.GetIndex(ctx, cfg.repo())
	if err != nil {
		return nil, err
	}
	return loadIndex(raw, sig, cfg.Verifier, cfg.repo())
}

func List(ctx context.Context, w io.Writer, cfg Config, prefix string) error {
	if _, err := normalizePrefix(prefix); err != nil {
		return err
	}
	idx, err := fetchIndex(ctx, cfg)
	if err != nil {
		return err
	}
	entries, err := filterEntries(idx, prefix)
	if err != nil {
		return err
	}
	for _, e := range entries {
		switch e.GetKind() {
		case indexpb.EntryKind_ENTRY_KIND_SYMLINK:
			if _, err := fmt.Fprintf(w, "l %04o %8d %s -> %s\n", e.GetMode(), e.GetSize(), e.GetPath(), e.GetSymlinkTarget()); err != nil {
				return err
			}
		default:
			if _, err := fmt.Fprintf(w, "f %04o %8d %s\n", e.GetMode(), e.GetSize(), e.GetPath()); err != nil {
				return err
			}
		}
	}
	return nil
}
