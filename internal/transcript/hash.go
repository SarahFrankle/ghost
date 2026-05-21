package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// ContentHash returns "sha256:<hex>" for the file's full contents.
func ContentHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
