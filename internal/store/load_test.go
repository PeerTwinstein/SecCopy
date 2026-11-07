package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"seccopy/internal/manifest"
)

type fakeVerifier struct{}

func (fakeVerifier) Verify(index, sig []byte) error {
	if !bytes.HasPrefix(sig, []byte("SIG:")) || !bytes.Equal(sig[4:], index) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

type fakeSigner struct{}

func (fakeSigner) Sign(data []byte) ([]byte, error) {
	out := make([]byte, len(data)+4)
	copy(out, []byte("SIG:"))
	copy(out[4:], data)
	return out, nil
}

func TestLoadOffersMatchingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "nested.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Publish(root, "default", fakeSigner{}); err != nil {
		t.Fatal(err)
	}

	st, err := Load(root, "default", fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Offered) != 2 || st.Skipped != 0 {
		t.Fatalf("offered=%d skipped=%d", len(st.Offered), st.Skipped)
	}
	if _, ok := st.Offered["ok.txt"]; !ok {
		t.Fatal("missing ok.txt")
	}
}

func TestLoadSkipsHashMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tamper.txt"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Publish(root, "default", fakeSigner{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tamper.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Load(root, "default", fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Skipped != 1 {
		t.Fatalf("skipped=%d", st.Skipped)
	}
	if _, ok := st.Offered["tamper.txt"]; ok {
		t.Fatal("tampered file must not be offered")
	}
	if _, ok := st.Offered["keep.txt"]; !ok {
		t.Fatal("untouched file must still be offered")
	}
}

func TestLoadRejectsBadSignature(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Publish(root, "default", fakeSigner{}); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root, "default", fakeVerifierFail{})
	if err == nil {
		t.Fatal("expected signature failure")
	}
}

type fakeVerifierFail struct{}

func (fakeVerifierFail) Verify(_, _ []byte) error { return fmt.Errorf("nope") }
