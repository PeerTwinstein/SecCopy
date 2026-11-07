package client

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"seccopy/internal/indexpb"
	"seccopy/internal/manifest"
	"seccopy/internal/sign"
	"seccopy/internal/tree"
)

func matchPrefix(path, prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return "", nil
	}
	if !tree.ValidRelPath(prefix) {
		return "", fmt.Errorf("invalid path prefix %q", prefix)
	}
	return prefix, nil
}

func loadIndex(index, sig []byte, verifier sign.Verifier, repo string) (*indexpb.Index, error) {
	if verifier == nil {
		return nil, fmt.Errorf("verifier is required")
	}
	if err := verifier.Verify(index, sig); err != nil {
		return nil, fmt.Errorf("index signature: %w", err)
	}
	var idx indexpb.Index
	if err := proto.Unmarshal(index, &idx); err != nil {
		return nil, fmt.Errorf("index protobuf: %w", err)
	}
	if idx.GetFormatVersion() != manifest.FormatVersion {
		return nil, fmt.Errorf("unsupported index format version %d", idx.GetFormatVersion())
	}
	if repo == "" {
		repo = "default"
	}
	if idx.GetRepo() != repo {
		return nil, fmt.Errorf("repository name mismatch: index has %q, want %q", idx.GetRepo(), repo)
	}
	return &idx, nil
}

func filterEntries(idx *indexpb.Index, prefix string) ([]*indexpb.Entry, error) {
	p, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	out := make([]*indexpb.Entry, 0, len(idx.GetEntries()))
	for _, e := range idx.GetEntries() {
		if !tree.ValidRelPath(e.GetPath()) {
			return nil, fmt.Errorf("%s: illegal path in index", e.GetPath())
		}
		if matchPrefix(e.GetPath(), p) {
			out = append(out, e)
		}
	}
	return out, nil
}
