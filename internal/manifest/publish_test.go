package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"seccopy/internal/indexpb"
	"seccopy/internal/tree"
)

type fakeSigner struct {
	sig []byte
	err error
}

func (f fakeSigner) Sign(data []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.sig) > 0 {
		return f.sig, nil
	}
	out := make([]byte, len(data)+4)
	copy(out, []byte("SIG:"))
	copy(out[4:], data)
	return out, nil
}

func TestPublishWritesIndexLast(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := Publish(root, "default", fakeSigner{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("entries=%d", n)
	}

	raw, err := os.ReadFile(filepath.Join(root, tree.IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(root, tree.IndexSigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(sig, []byte("SIG:")) {
		t.Fatalf("unexpected signature %q", sig[:min(8, len(sig))])
	}

	var idx indexpb.Index
	if err := proto.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.GetRepo() != "default" || idx.GetFormatVersion() != FormatVersion {
		t.Fatalf("index header: %+v", &idx)
	}
	if len(idx.Entries) != 1 || idx.Entries[0].GetPath() != "hello.txt" {
		t.Fatalf("entries: %+v", idx.Entries)
	}
	if idx.Entries[0].GetKind() != indexpb.EntryKind_ENTRY_KIND_FILE {
		t.Fatal("expected file kind")
	}
}

func TestPublishReplacesExistingIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(root, "default", fakeSigner{sig: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(root, "default", fakeSigner{sig: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(root, tree.IndexSigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(sig) != "new" {
		t.Fatalf("sig=%q", sig)
	}
	raw, _ := os.ReadFile(filepath.Join(root, tree.IndexFileName))
	var idx indexpb.Index
	if err := proto.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(idx.Entries))
	}
}

func TestPublishFailsBeforeWritingOnBadTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "bad")); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	_, err := Publish(root, "default", fakeSigner{sig: []byte("should-not-write")})
	if err == nil || !strings.Contains(err.Error(), "escapes the tree") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, tree.IndexFileName)); !os.IsNotExist(statErr) {
		t.Fatal("index.bin must not be written when scan fails")
	}
}
