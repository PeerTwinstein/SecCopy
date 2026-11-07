package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"seccopy/internal/sign"
	"seccopy/internal/tree"
)

func Publish(root, repo string, signer sign.Signer) (int, error) {
	if signer == nil {
		return 0, fmt.Errorf("signer is required")
	}
	entries, err := tree.Scan(root)
	if err != nil {
		return 0, err
	}
	raw, err := Marshal(Build(repo, entries))
	if err != nil {
		return 0, err
	}
	sig, err := signer.Sign(raw)
	if err != nil {
		return 0, fmt.Errorf("sign index: %w", err)
	}
	if err := WriteAtomic(root, raw, sig); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func WriteAtomic(root string, index, sig []byte) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(absRoot, tree.IndexFileName)
	sigPath := filepath.Join(absRoot, tree.IndexSigFileName)
	indexTmp := indexPath + ".tmp"
	sigTmp := sigPath + ".tmp"

	if err := writeFileSync(indexTmp, index); err != nil {
		return err
	}
	if err := writeFileSync(sigTmp, sig); err != nil {
		os.Remove(indexTmp)
		return err
	}
	if err := renameOver(indexTmp, indexPath); err != nil {
		os.Remove(indexTmp)
		os.Remove(sigTmp)
		return err
	}
	if err := renameOver(sigTmp, sigPath); err != nil {
		os.Remove(sigTmp)
		return err
	}
	return nil
}

func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
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
