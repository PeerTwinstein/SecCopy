package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidRelPath reports whether p is a safe slash-separated relative path.
func ValidRelPath(p string) bool {
	if p == "" || p == "." || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// CheckSymlinkInRoot ensures the symlink target stays inside root.
// Absolute targets are rejected; so are any that lexically escape the tree.
func CheckSymlinkInRoot(root, linkPath, target string) error {
	if target == "" {
		return fmt.Errorf("empty symlink target")
	}
	slash := filepath.ToSlash(target)
	// Treat Unix-style absolute paths as escaping even when running on Windows.
	if filepath.IsAbs(target) || strings.HasPrefix(slash, "/") {
		return fmt.Errorf("symlink target %q escapes the tree", target)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absResolved)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("symlink target %q escapes the tree", target)
	}
	return nil
}

// ErrPathTraversal is returned when a path would leave the data root.
var ErrPathTraversal = fmt.Errorf("path traversal")

// ResolveInRoot joins rel (slash-separated) onto root and ensures the result
// stays inside root. The file need not exist.
func ResolveInRoot(root, rel string) (string, error) {
	if !ValidRelPath(rel) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absRoot, filepath.FromSlash(rel))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	got, err := filepath.Rel(absRoot, absFull)
	if err != nil {
		return "", err
	}
	if got == ".." || strings.HasPrefix(got, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s", ErrPathTraversal, rel)
	}
	return absFull, nil
}

// UnixMode returns permission bits plus setuid/setgid/sticky.
func UnixMode(m os.FileMode) uint32 {
	u := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		u |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		u |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		u |= 0o1000
	}
	return u
}

// FileModeFromUnix converts stored Unix mode bits back to os.FileMode.
func FileModeFromUnix(u uint32) os.FileMode {
	m := os.FileMode(u & 0o777)
	if u&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if u&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if u&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return m
}
