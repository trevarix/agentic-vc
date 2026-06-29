package branch

import (
	"io"
	"os"
	"path/filepath"
)

// tryHardlink attempts to create a hard link from src to dst.
// Returns an error if the link cannot be created (different device, permissions, etc.)
func tryHardlink(src, dst string) error {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Link(src, dst)
}

// regularCopy copies src to dst byte-for-byte.
// This is the universal fallback used when hardlinks and reflinks are unavailable.
func regularCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
