package tree

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zeebo/blake3"
)

func TestScanRegularFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("hello"), 0o644)
	mustWrite(t, filepath.Join(root, "sub", "b.bin"), []byte{0x00, 0x01}, 0o755)

	entries, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Path != "a.txt" || entries[1].Path != "sub/b.bin" {
		t.Fatalf("paths: %+v", []string{entries[0].Path, entries[1].Path})
	}
	if entries[0].Hash != blake3.Sum256([]byte("hello")) {
		t.Fatal("hash mismatch for a.txt")
	}
	if entries[0].Size != 5 {
		t.Fatalf("size=%d", entries[0].Size)
	}
	if entries[0].Kind != KindFile || entries[1].Kind != KindFile {
		t.Fatal("expected files")
	}
	if entries[1].Mode&0o111 == 0 && runtime.GOOS != "windows" {
		t.Fatalf("expected executable bit on b.bin, mode=%o", entries[1].Mode)
	}
}

func TestScanIgnoresEmptyDirsAndIndexFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "keep.txt"), []byte("x"), 0o644)
	mustWrite(t, filepath.Join(root, IndexFileName), []byte("old"), 0o644)
	mustWrite(t, filepath.Join(root, IndexSigFileName), []byte("oldsig"), 0o644)

	entries, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "keep.txt" {
		t.Fatalf("got %+v", entries)
	}
}

func TestScanSymlinkInsideTree(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "target.txt"), []byte("data"), 0o644)
	if err := os.Symlink("target.txt", filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	entries, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var link *Entry
	for i := range entries {
		if entries[i].Path == "link" {
			link = &entries[i]
		}
	}
	if link == nil {
		t.Fatal("missing symlink entry")
	}
	if link.Kind != KindSymlink || link.SymlinkTarget != "target.txt" {
		t.Fatalf("symlink: %+v", link)
	}
	want := blake3.Sum256([]byte("target.txt"))
	if link.Hash != want {
		t.Fatal("symlink hash should be blake3 of target string")
	}
}

func TestScanRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	mustWrite(t, outside, []byte("nope"), 0o644)
	t.Cleanup(func() { os.Remove(outside) })

	if err := os.Symlink(filepath.Join("..", "outside.txt"), filepath.Join(root, "bad")); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	_, err := Scan(root)
	if err == nil || !strings.Contains(err.Error(), "escapes the tree") {
		t.Fatalf("got %v, want escapes the tree", err)
	}
}

func TestScanRejectsAbsoluteSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "bad")); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	_, err := Scan(root)
	if err == nil || !strings.Contains(err.Error(), "escapes the tree") {
		t.Fatalf("got %v, want escapes the tree", err)
	}
}

func TestScanRejectsHardlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "orig")
	mustWrite(t, src, []byte("same"), 0o644)
	if err := os.Link(src, filepath.Join(root, "alias")); err != nil {
		t.Skip("hardlinks not supported:", err)
	}
	_, err := Scan(root)
	if err == nil || !strings.Contains(err.Error(), "hardlinks") {
		t.Fatalf("got %v, want hardlinks rejected", err)
	}
}

func TestResolveInRoot(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveInRoot(root, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "a", "b.txt")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	for _, p := range []string{"../x", "..", "/etc/passwd", "a/../../etc/passwd", ""} {
		if _, err := ResolveInRoot(root, p); !errors.Is(err, ErrPathTraversal) {
			t.Errorf("%q: got %v, want traversal", p, err)
		}
	}
}

func TestValidRelPath(t *testing.T) {
	ok := []string{"a", "a/b", "a/b/c.txt"}
	bad := []string{"", ".", "..", "../x", "a/../b", "/a", "a//b", "a/./b"}
	for _, p := range ok {
		if !ValidRelPath(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range bad {
		if ValidRelPath(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
