package service

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func fingerprint(binary string) (string, error) {
	path, e := exec.LookPath(binary)
	if e != nil {
		return "", e
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("engine must be a regular executable")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, 256<<20+1))
	if e != nil {
		return "", e
	}
	if n > 256<<20 {
		return "", fmt.Errorf("engine binary exceeds limit")
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
