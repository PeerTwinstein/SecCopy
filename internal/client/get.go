package client

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zeebo/blake3"

	"seccopy/internal/indexpb"
	"seccopy/internal/tree"
)

func Get(ctx context.Context, cfg Config, dest, prefix string) (int, error) {
	if dest == "" {
		return 0, fmt.Errorf("destination directory is required")
	}
	if _, err := normalizePrefix(prefix); err != nil {
		return 0, err
	}
	idx, err := fetchIndex(ctx, cfg)
	if err != nil {
		return 0, err
	}
	entries, err := filterEntries(idx, prefix)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, fmt.Errorf("no matching entries")
	}

	absDest, err := filepath.Abs(dest)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(absDest, 0o755); err != nil {
		return 0, err
	}
	staging, err := os.MkdirTemp(absDest, ".seccopy-*")
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return 0, err
	}
	sess := &session{staging: staging}
	n, err := sess.materialize(ctx, cfg, absDest, entries)
	if err != nil {
		return 0, sess.abort(err)
	}
	_ = os.RemoveAll(staging)
	return n, nil
}

func (s *session) materialize(ctx context.Context, cfg Config, dest string, entries []*indexpb.Entry) (int, error) {
	var files, links []*indexpb.Entry
	for _, e := range entries {
		switch e.GetKind() {
		case indexpb.EntryKind_ENTRY_KIND_FILE:
			files = append(files, e)
		case indexpb.EntryKind_ENTRY_KIND_SYMLINK:
			links = append(links, e)
		default:
			return 0, fmt.Errorf("%s: unknown entry kind", e.GetPath())
		}
	}
	for _, e := range files {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := s.getFile(ctx, cfg, dest, e); err != nil {
			return 0, err
		}
	}
	for _, e := range links {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := s.getLink(dest, e); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func (s *session) getFile(ctx context.Context, cfg Config, dest string, e *indexpb.Entry) error {
	final, err := tree.ResolveInRoot(dest, e.GetPath())
	if err != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), err)
	}
	tmp, err := os.CreateTemp(s.staging, "part-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	s.rememberTemp(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}

	body, err := cfg.Fetcher.GetFile(ctx, cfg.repo(), e.GetPath())
	if err != nil {
		tmp.Close()
		return fmt.Errorf("%s: %w", e.GetPath(), err)
	}
	h := blake3.New()
	_, copyErr := copyLimited(io.MultiWriter(tmp, h), body, e.GetSize())
	closeErr := body.Close()
	syncErr := tmp.Sync()
	_ = tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), closeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), syncErr)
	}

	sum := h.Sum(nil)
	if subtle.ConstantTimeCompare(sum, e.GetBlake3()) != 1 {
		return fmt.Errorf("%s: blake3 mismatch", e.GetPath())
	}

	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	if err := renameOver(tmpName, final); err != nil {
		return err
	}
	s.temps = s.temps[:len(s.temps)-1]
	s.rememberPlaced(final)
	if err := os.Chmod(final, tree.FileModeFromUnix(e.GetMode())); err != nil {
		return err
	}
	return nil
}

func (s *session) getLink(dest string, e *indexpb.Entry) error {
	final, err := tree.ResolveInRoot(dest, e.GetPath())
	if err != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), err)
	}
	target := e.GetSymlinkTarget()
	if err := tree.CheckSymlinkInRoot(dest, final, target); err != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), err)
	}
	sum := blake3.Sum256([]byte(target))
	if uint64(len(target)) != e.GetSize() || subtle.ConstantTimeCompare(sum[:], e.GetBlake3()) != 1 {
		return fmt.Errorf("%s: symlink metadata mismatch", e.GetPath())
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	_ = os.Remove(final)
	if err := os.Symlink(target, final); err != nil {
		return fmt.Errorf("%s: %w", e.GetPath(), err)
	}
	s.rememberPlaced(final)
	return nil
}

func renameOver(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}
