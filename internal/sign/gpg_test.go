package sign

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGPGSignAndVerify(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed")
	}
	home := t.TempDir()
	t.Setenv("GNUPGHOME", home)
	// Restrictive home is required by gpg.
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}

	gen := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-generate-key", "seccopy-test <test@seccopy.local>", "ed25519", "sign", "never")
	gen.Env = append(os.Environ(), "GNUPGHOME="+home)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("could not create test key: %v\n%s", err, out)
	}

	data := []byte("index-bytes")
	sig, err := (GPG{User: "test@seccopy.local"}).Sign(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) == 0 {
		t.Fatal("empty signature")
	}

	payload := filepath.Join(home, "index.bin")
	sigFile := payload + ".sig"
	if err := os.WriteFile(payload, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigFile, sig, 0o600); err != nil {
		t.Fatal(err)
	}
	verify := exec.Command("gpg", "--batch", "--verify", sigFile, payload)
	verify.Env = append(os.Environ(), "GNUPGHOME="+home)
	out, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("verify failed: %v\n%s", err, out)
	}
	if err := (GPG{User: "test@seccopy.local"}).Verify(data, sig); err != nil {
		t.Fatal(err)
	}
	if err := (GPG{User: "test@seccopy.local"}).Verify(data, append([]byte{0}, sig...)); err == nil {
		t.Fatal("expected verify failure on bad signature")
	}
	if !bytes.Contains(out, []byte("Good signature")) && !bytes.Contains(out, []byte("gute Signatur")) {
		// gpg writes result to stderr; CombinedOutput should still contain it.
		t.Logf("gpg verify output: %s", out)
	}
}
