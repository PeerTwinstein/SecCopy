package tree

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeebo/blake3"
)

const (
	IndexFileName    = "index.bin"
	IndexSigFileName = "index.bin.sig"
)

type Kind int

const (
	KindFile Kind = iota
	KindSymlink
)

type Entry struct {
	Path          string
	Hash          [32]byte
	Size          uint64
	Mode          uint32
	Kind          Kind
	SymlinkTarget string
}

func Scan(root string) ([]Entry, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", absRoot)
	}

	indexPath := filepath.Join(absRoot, IndexFileName)
	sigPath := filepath.Join(absRoot, IndexSigFileName)
	indexTmp := indexPath + ".tmp"
	sigTmp := sigPath + ".tmp"

	var entries []Entry
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == absRoot {
			return nil
		}
		switch path {
		case indexPath, sigPath, indexTmp, sigTmp:
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if !ValidRelPath(relSlash) {
			return fmt.Errorf("%s: illegal path", relSlash)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()

		switch {
		case mode&os.ModeSymlink != 0:
			e, err := scanSymlink(absRoot, path, relSlash, info)
			if err != nil {
				return err
			}
			entries = append(entries, e)
		case mode.IsDir():
			return nil
		case !mode.IsRegular():
			return fmt.Errorf("%s: unsupported file type (%s)", relSlash, typeName(mode))
		default:
			n, err := nlinks(path, info)
			if err != nil {
				return err
			}
			if n > 1 {
				return fmt.Errorf("%s: hardlinks are not allowed", relSlash)
			}
			e, err := scanFile(path, relSlash, info)
			if err != nil {
				return err
			}
			entries = append(entries, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return strings.Compare(a.Path, b.Path)
	})
	return entries, nil
}

func HashFile(path string) ([32]byte, uint64, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return [32]byte{}, 0, err
	}
	defer f.Close()

	h := blake3.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return [32]byte{}, 0, err
	}
	var sum [32]byte
	h.Sum(sum[:0])
	return sum, uint64(n), nil
}

func scanFile(path, rel string, info os.FileInfo) (Entry, error) {
	sum, n, err := HashFile(path)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", rel, err)
	}
	if n != uint64(info.Size()) {
		return Entry{}, fmt.Errorf("%s: size changed during read", rel)
	}
	return Entry{
		Path: rel,
		Hash: sum,
		Size: n,
		Mode: UnixMode(info.Mode()),
		Kind: KindFile,
	}, nil
}

func scanSymlink(root, path, rel string, info os.FileInfo) (Entry, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return Entry{}, err
	}
	if err := CheckSymlinkInRoot(root, path, target); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", rel, err)
	}
	sum := blake3.Sum256([]byte(target))
	return Entry{
		Path:          rel,
		Hash:          sum,
		Size:          uint64(len(target)),
		Mode:          UnixMode(info.Mode()),
		Kind:          KindSymlink,
		SymlinkTarget: target,
	}, nil
}

func typeName(mode os.FileMode) string {
	switch mode.Type() {
	case os.ModeDir:
		return "directory"
	case os.ModeSymlink:
		return "symlink"
	case os.ModeDevice:
		return "device"
	case os.ModeNamedPipe:
		return "fifo"
	case os.ModeSocket:
		return "socket"
	case os.ModeCharDevice:
		return "char device"
	case os.ModeIrregular:
		return "irregular"
	default:
		return mode.Type().String()
	}
}
