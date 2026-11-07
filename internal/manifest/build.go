package manifest

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"seccopy/internal/indexpb"
	"seccopy/internal/tree"
)

const FormatVersion = 1

func Build(repo string, entries []tree.Entry) *indexpb.Index {
	if repo == "" {
		repo = "default"
	}
	idx := &indexpb.Index{
		FormatVersion: FormatVersion,
		Repo:          repo,
		Entries:       make([]*indexpb.Entry, 0, len(entries)),
	}
	for _, e := range entries {
		pe := &indexpb.Entry{
			Path:   e.Path,
			Blake3: append([]byte(nil), e.Hash[:]...),
			Size:   e.Size,
			Mode:   e.Mode,
		}
		switch e.Kind {
		case tree.KindFile:
			pe.Kind = indexpb.EntryKind_ENTRY_KIND_FILE
		case tree.KindSymlink:
			pe.Kind = indexpb.EntryKind_ENTRY_KIND_SYMLINK
			pe.SymlinkTarget = e.SymlinkTarget
		default:
			continue
		}
		idx.Entries = append(idx.Entries, pe)
	}
	return idx
}

func Marshal(idx *indexpb.Index) ([]byte, error) {
	if idx.GetFormatVersion() == 0 {
		return nil, fmt.Errorf("missing index format version")
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(idx)
}
