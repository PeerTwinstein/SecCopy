package sign

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Signer interface {
	Sign(data []byte) ([]byte, error)
}

type Verifier interface {
	Verify(index, signature []byte) error
}

type GPG struct {
	Bin  string
	User string
}

func (g GPG) Sign(data []byte) ([]byte, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gpg"
	}
	args := []string{"--batch", "--yes", "--detach-sign", "-o", "-"}
	if g.User != "" {
		args = append(args, "--local-user", g.User)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return nil, fmt.Errorf("gpg: %s", msg)
		}
		return nil, fmt.Errorf("gpg: %w", err)
	}
	sig := stdout.Bytes()
	if len(sig) == 0 {
		return nil, fmt.Errorf("gpg: empty signature")
	}
	return sig, nil
}

func (g GPG) Verify(index, signature []byte) error {
	if len(index) == 0 || len(signature) == 0 {
		return fmt.Errorf("gpg: missing index or signature")
	}
	dir, err := os.MkdirTemp("", "seccopy-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	dataPath := filepath.Join(dir, "index.bin")
	sigPath := filepath.Join(dir, "index.bin.sig")
	if err := os.WriteFile(dataPath, index, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(sigPath, signature, 0o600); err != nil {
		return err
	}

	bin := g.Bin
	if bin == "" {
		bin = "gpg"
	}
	args := []string{"--batch", "--status-fd", "2", "--verify", sigPath, dataPath}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return fmt.Errorf("gpg verify: %s", msg)
		}
		return fmt.Errorf("gpg verify: %w", err)
	}
	if g.User != "" && !validSigner(stderr.Bytes(), g.User) {
		return fmt.Errorf("gpg verify: signature is not from %s", g.User)
	}
	return nil
}

func validSigner(status []byte, user string) bool {
	want := strings.ToUpper(strings.TrimSpace(user))
	want = strings.ReplaceAll(want, " ", "")
	for _, line := range bytes.Split(status, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("[GNUPG:] VALIDSIG ")) {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) < 3 {
			continue
		}
		got := strings.ToUpper(string(fields[2]))
		if got == want || strings.HasSuffix(got, want) || strings.Contains(strings.ToUpper(string(line)), want) {
			return true
		}
	}
	// Key id / email may only appear on GOODSIG, not as VALIDSIG fingerprint.
	needle := []byte(strings.ToUpper(strings.TrimSpace(user)))
	for _, line := range bytes.Split(status, []byte("\n")) {
		u := bytes.ToUpper(line)
		if bytes.Contains(u, []byte("GOODSIG")) && bytes.Contains(u, needle) {
			return true
		}
	}
	return false
}
