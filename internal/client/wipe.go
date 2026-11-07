package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const zeroBufSize = 64 * 1024

func wipePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return os.Remove(path)
	}
	if err := zeroRegular(path, info.Size()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return os.Remove(path)
}

func zeroRegular(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, zeroBufSize)
	var left int64 = size
	for left > 0 {
		n := len(buf)
		if int64(n) > left {
			n = int(left)
		}
		wn, err := f.Write(buf[:n])
		if err != nil {
			return err
		}
		left -= int64(wn)
	}
	return f.Sync()
}

func wipeDir(dir string) error {
	if dir == "" {
		return nil
	}
	var first error
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if werr := wipePath(path); werr != nil && first == nil {
			first = werr
		}
		return nil
	})
	if err := os.RemoveAll(dir); err != nil && first == nil {
		first = err
	}
	return first
}

type session struct {
	staging string
	placed  []string
	temps   []string
}

func (s *session) rememberTemp(path string) {
	s.temps = append(s.temps, path)
}

func (s *session) rememberPlaced(path string) {
	s.placed = append(s.placed, path)
}

func (s *session) abort(err error) error {
	s.wipe()
	if err == nil {
		return fmt.Errorf("download aborted")
	}
	return err
}

func (s *session) wipe() {
	for i := len(s.temps) - 1; i >= 0; i-- {
		_ = wipePath(s.temps[i])
	}
	for i := len(s.placed) - 1; i >= 0; i-- {
		_ = wipePath(s.placed[i])
	}
	_ = wipeDir(s.staging)
}

func copyLimited(w io.Writer, r io.Reader, size uint64) (uint64, error) {
	n, err := io.Copy(w, io.LimitReader(r, int64(size)+1))
	if err != nil {
		return uint64(n), err
	}
	if uint64(n) != size {
		return uint64(n), fmt.Errorf("size mismatch: got %d, want %d", n, size)
	}
	return uint64(n), nil
}
