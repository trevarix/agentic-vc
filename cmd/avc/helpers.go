package avc

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveProjectPath converts a potentially relative path to an absolute one.
func resolveProjectPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid project path: %w", err)
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		return "", fmt.Errorf("project path does not exist: %s", abs)
	}
	return abs, nil
}

// requireInitializedProject finds the .avc directory starting from the current
// working directory and walking up the tree. Returns the project root path.
func requireInitializedProject() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		avcDir := filepath.Join(dir, ".avc")
		if info, err := os.Stat(avcDir); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("no AVC project found (run `avc init` first)")
}
