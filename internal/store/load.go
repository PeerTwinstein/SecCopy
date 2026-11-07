package store

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/zeebo/blake3"
	"google.golang.org/protobuf/proto"

	"seccopy/internal/indexpb"
	"seccopy/internal/manifest"
	"seccopy/internal/sign"
	"seccopy/internal/tree"
)

type Store struct {
	Root    string
	Name    string
	Index   []byte
	Sig     []byte
	Offered map[string]*indexpb.Entry
	Skipped int
}

func Load(root, repo string, verifier sign.Verifier) (*Store, error) {
	if verifier == nil {
		return nil, fmt.Errorf("verifier is required")
	}
	if repo == "" {
		repo = "default"
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	indexRaw, err := os.ReadFile(filepath.Join(absRoot, tree.IndexFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tree.IndexFileName, err)
	}
	sigRaw, err := os.ReadFile(filepath.Join(absRoot, tree.IndexSigFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tree.IndexSigFileName, err)
	}
	if err := verifier.Verify(indexRaw, sigRaw); err != nil {
		return nil, fmt.Errorf("index signature: %w", err)
	}

	var idx indexpb.Index
	if err := proto.Unmarshal(indexRaw, &idx); err != nil {
		return nil, fmt.Errorf("index protobuf: %w", err)
	}
	if idx.GetFormatVersion() != manifest.FormatVersion {
		return nil, fmt.Errorf("unsupported index format version %d", idx.GetFormatVersion())
	}
	if idx.GetRepo() != repo {
		return nil, fmt.Errorf("repository name mismatch: index has %q, want %q", idx.GetRepo(), repo)
	}

	offered, skipped, err := verifyTree(absRoot, idx.GetEntries())
	if err != nil {
		return nil, err
	}
	return &Store{
		Root:    absRoot,
		Name:    repo,
		Index:   indexRaw,
		Sig:     sigRaw,
		Offered: offered,
		Skipped: skipped,
	}, nil
}

func verifyTree(root string, entries []*indexpb.Entry) (map[string]*indexpb.Entry, int, error) {
	offered := make(map[string]*indexpb.Entry, len(entries))
	if len(entries) == 0 {
		return offered, 0, nil
	}

	workers := runtime.GOMAXPROCS(0) * 2
	if workers < 2 {
		workers = 2
	}
	if workers > len(entries) {
		workers = len(entries)
	}

	type outcome struct {
		entry  *indexpb.Entry
		skip   bool
		secErr error
	}

	work := make(chan *indexpb.Entry)
	out := make(chan outcome, len(entries))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range work {
				skip, err := verifyEntry(root, e)
				out <- outcome{entry: e, skip: skip, secErr: err}
			}
		}()
	}
	go func() {
		for _, e := range entries {
			work <- e
		}
		close(work)
		wg.Wait()
		close(out)
	}()

	skipped := 0
	var firstSec error
	for oc := range out {
		if oc.secErr != nil {
			if firstSec == nil {
				firstSec = oc.secErr
			}
			continue
		}
		if oc.skip {
			skipped++
			continue
		}
		offered[oc.entry.GetPath()] = oc.entry
	}
	if firstSec != nil {
		return nil, 0, firstSec
	}
	return offered, skipped, nil
}

func verifyEntry(root string, e *indexpb.Entry) (skip bool, err error) {
	path := e.GetPath()
	full, err := tree.ResolveInRoot(root, path)
	if err != nil {
		slog.Error("security", "event", "path_traversal", "path", path, "err", err)
		return false, fmt.Errorf("%s: %w", path, err)
	}

	switch e.GetKind() {
	case indexpb.EntryKind_ENTRY_KIND_SYMLINK:
		target, err := os.Readlink(full)
		if err != nil {
			slog.Error("hash mismatch", "path", path, "err", err)
			return true, nil
		}
		if err := tree.CheckSymlinkInRoot(root, full, target); err != nil {
			slog.Error("security", "event", "symlink_escape", "path", path, "target", target)
			return false, fmt.Errorf("%s: %w", path, err)
		}
		if target != e.GetSymlinkTarget() {
			slog.Error("hash mismatch", "path", path, "reason", "symlink target changed")
			return true, nil
		}
		sum := blake3.Sum256([]byte(target))
		if uint64(len(target)) != e.GetSize() || subtle.ConstantTimeCompare(sum[:], e.GetBlake3()) != 1 {
			slog.Error("hash mismatch", "path", path, "reason", "symlink metadata")
			return true, nil
		}
		return false, nil

	case indexpb.EntryKind_ENTRY_KIND_FILE:
		sum, size, err := tree.HashFile(full)
		if err != nil {
			slog.Error("hash mismatch", "path", path, "err", err)
			return true, nil
		}
		if size != e.GetSize() || subtle.ConstantTimeCompare(sum[:], e.GetBlake3()) != 1 {
			slog.Error("hash mismatch", "path", path, "size", size, "expected_size", e.GetSize())
			return true, nil
		}
		return false, nil

	default:
		slog.Error("hash mismatch", "path", path, "reason", "unknown kind")
		return true, nil
	}
}
