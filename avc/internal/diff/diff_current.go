// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package diff

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
)

// CompareWithCurrent computes the diff between a snapshot and the current
// working tree on disk. sourceDir is the directory to walk (pass "" to use
// projectRoot, which is the default for main; pass the workspace path for
// agent branches so the diff reflects the workspace, not the project root).
func CompareWithCurrent(projectRoot, snapshotID string) (*Result, error) {
	return CompareWithCurrentDir(projectRoot, projectRoot, snapshotID)
}

// CompareWithCurrentDir is the workspace-aware variant of CompareWithCurrent.
// sourceDir is walked for the current file state; projectRoot provides the
// object store and ignore rules. On main, pass projectRoot for both.
func CompareWithCurrentDir(projectRoot, sourceDir, snapshotID string) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	snapFiles, err := store.GetSnapshotFiles(snapshotID)
	if err != nil {
		return nil, err
	}

	snapMap := filesByPath(snapFiles)

	// Walk current project to get disk state.
	ignore, err := fileutil.LoadIgnoreRules(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load ignore rules: %w", err)
	}

	currentPaths, err := fileutil.WalkProject(sourceDir, ignore)
	if err != nil {
		return nil, fmt.Errorf("walk project: %w", err)
	}

	// Build a map of current files: relative path → hash.
	type currentFile struct {
		hash    string
		absPath string
	}
	currentMap := make(map[string]*currentFile, len(currentPaths))
	for _, absPath := range currentPaths {
		rel, err := filepath.Rel(sourceDir, absPath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)

		hash, err := fileutil.HashFile(absPath)
		if err != nil {
			continue
		}
		currentMap[rel] = &currentFile{hash: hash, absPath: absPath}
	}

	var diffs []*FileDiff

	// Files in current tree: check for added or modified.
	for path, cf := range currentMap {
		snapFile, exists := snapMap[path]
		if !exists {
			fd := &FileDiff{
				Path:    path,
				Type:    Added,
				NewHash: cf.hash,
			}
			enrichWithLineCountsDisk(projectRoot, fd, cf.absPath)
			diffs = append(diffs, fd)
			continue
		}
		if snapFile.FileHash != cf.hash {
			fd := &FileDiff{
				Path:    path,
				Type:    Modified,
				OldHash: snapFile.FileHash,
				NewHash: cf.hash,
			}
			enrichWithLineCountsDisk(projectRoot, fd, cf.absPath)
			diffs = append(diffs, fd)
		}
	}

	// Files in snapshot but not on disk: deleted.
	for path, snapFile := range snapMap {
		if _, exists := currentMap[path]; !exists {
			fd := &FileDiff{
				Path:    path,
				Type:    Deleted,
				OldHash: snapFile.FileHash,
			}
			enrichWithLineCounts(projectRoot, fd)
			diffs = append(diffs, fd)
		}
	}

	sortDiffs(diffs)

	return &Result{
		FromSnapshotID: snapshotID,
		ToSnapshotID:   "working-tree",
		Files:          diffs,
	}, nil
}

// enrichWithLineCountsDisk computes line-level diff stats where the "new"
// content is read from disk rather than the object store.
func enrichWithLineCountsDisk(projectRoot string, fd *FileDiff, currentFilePath string) {
	oldData := ReadObjectSafe(projectRoot, fd.OldHash)
	newData, _ := os.ReadFile(currentFilePath)

	if IsBinary(oldData) || IsBinary(newData) {
		fd.Binary = true
		return
	}

	added, removed, preview, estimated := computeUnifiedDiff(SplitLines(oldData), SplitLines(newData))
	fd.LinesAdded = added
	fd.LinesRemoved = removed
	fd.DiffPreview = preview
	fd.CountsEstimated = estimated
}
