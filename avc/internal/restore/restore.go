// Package restore handles rolling back a project to a previous snapshot.
package restore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
)

// Result is returned by Restore on success.
type Result struct {
	SnapshotID    string
	RestoredFiles int
	RestoredSize  int64
}

// Restore rolls the project back to the state captured in snapshotID.
// Files in the snapshot are written back. Files tracked in a more recent
// snapshot but absent from the target snapshot are deleted from disk.
func Restore(projectRoot, snapshotID string) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	targetFiles, err := store.GetSnapshotFiles(snapshotID)
	if err != nil {
		return nil, err
	}

	// Build a set of paths present in the target snapshot.
	targetPaths := make(map[string]bool, len(targetFiles))
	for _, f := range targetFiles {
		targetPaths[f.RelativePath] = true
	}

	// Find all files ever tracked across all snapshots so we know what to
	// consider for deletion (i.e. files added after the target snapshot).
	allSnapshots, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	for _, snap := range allSnapshots {
		if snap.ID == snapshotID {
			continue
		}
		snapFiles, err := store.GetSnapshotFiles(snap.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range snapFiles {
			if !targetPaths[f.RelativePath] {
				absPath := filepath.Join(projectRoot, filepath.FromSlash(f.RelativePath))
				if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
					return nil, fmt.Errorf("remove file %s: %w", f.RelativePath, err)
				}
			}
		}
	}

	// Write back every file from the target snapshot.
	var restoredSize int64
	for _, f := range targetFiles {
		absPath := filepath.Join(projectRoot, filepath.FromSlash(f.RelativePath))

		data, err := readObject(projectRoot, f.FileHash)
		if err != nil {
			return nil, fmt.Errorf("read object for %s: %w", f.RelativePath, err)
		}

		if err := fileutil.WriteFile(absPath, data); err != nil {
			return nil, fmt.Errorf("write file %s: %w", f.RelativePath, err)
		}
		restoredSize += f.FileSize
	}

	return &Result{
		SnapshotID:    snapshotID,
		RestoredFiles: len(targetFiles),
		RestoredSize:  restoredSize,
	}, nil
}

// objectPath returns the path inside .avc/objects/ where a file's content is stored.
// Files are addressed by their SHA256 hash (content-addressed storage).
func objectPath(projectRoot, hash string) string {
	// Shard by first two hex chars to avoid too many files in one directory.
	return filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
}

// readObject reads a stored file blob by its hash.
func readObject(projectRoot, hash string) ([]byte, error) {
	path := objectPath(projectRoot, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("object %s not found: %w", hash[:8], err)
	}
	return data, nil
}

// StoreObject writes file content to the object store under its hash.
// Called during snapshot creation to persist actual file bytes.
func StoreObject(projectRoot, hash string, data []byte) error {
	path := objectPath(projectRoot, hash)
	if _, err := os.Stat(path); err == nil {
		return nil // already stored (content-addressed deduplication)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
