package restore

import (
	"fmt"
	"path/filepath"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
)

// RestoreFileResult is returned on success of a single-file restore.
type RestoreFileResult struct {
	SnapshotID string
	FilePath   string
	Size       int64
}

// RestoreFile restores a single file from a snapshot to the project directory.
func RestoreFile(projectRoot, snapshotID, filePath string) (*RestoreFileResult, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	files, err := store.GetSnapshotFiles(snapshotID)
	if err != nil {
		return nil, err
	}

	// Normalize the requested path to forward slashes for comparison.
	normalizedPath := filepath.ToSlash(filePath)

	var target *db.File
	for _, f := range files {
		if f.RelativePath == normalizedPath {
			target = f
			break
		}
	}

	if target == nil {
		return nil, fmt.Errorf("file '%s' not found in snapshot '%s'", filePath, snapshotID)
	}

	data, err := readObject(projectRoot, target.FileHash)
	if err != nil {
		return nil, fmt.Errorf("read object for %s: %w", filePath, err)
	}

	absPath := filepath.Join(projectRoot, filepath.FromSlash(target.RelativePath))
	if err := fileutil.WriteFile(absPath, data); err != nil {
		return nil, fmt.Errorf("write file %s: %w", filePath, err)
	}

	return &RestoreFileResult{
		SnapshotID: snapshotID,
		FilePath:   target.RelativePath,
		Size:       target.FileSize,
	}, nil
}
