// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package restore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
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
	normalizedPath := filepath.ToSlash(filepath.Clean(filePath))

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
	if target.FileMode != 0 {
		_ = os.Chmod(absPath, os.FileMode(target.FileMode))
	}

	return &RestoreFileResult{
		SnapshotID: snapshotID,
		FilePath:   target.RelativePath,
		Size:       target.FileSize,
	}, nil
}

// RestoreFileToDir restores a single file from a snapshot into targetDir
// instead of the project root. Use this on agent branches to write into the
// workspace without touching the real project root.
// projectRoot still provides the object store and database location.
func RestoreFileToDir(projectRoot, snapshotID, filePath, targetDir string) (*RestoreFileResult, error) {
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

	normalizedPath := filepath.ToSlash(filepath.Clean(filePath))

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

	absPath := filepath.Join(targetDir, filepath.FromSlash(target.RelativePath))
	if err := fileutil.WriteFile(absPath, data); err != nil {
		return nil, fmt.Errorf("write file %s: %w", filePath, err)
	}
	if target.FileMode != 0 {
		_ = os.Chmod(absPath, os.FileMode(target.FileMode))
	}

	return &RestoreFileResult{
		SnapshotID: snapshotID,
		FilePath:   target.RelativePath,
		Size:       target.FileSize,
	}, nil
}
