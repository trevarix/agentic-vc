// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package restore handles rolling back a project to a previous snapshot.
package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/hooks"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// Result is returned by Restore on success.
type Result struct {
	SnapshotID    string
	RestoredFiles int
	RestoredSize  int64
}

// Restore rolls the project back to the state captured in snapshotID.
// Runs pre/post restore hooks if configured.
func Restore(projectRoot, snapshotID string) (*Result, error) {
	cfg, _ := config.Load(projectRoot)
	activeBranch := "main"
	if cfg != nil && cfg.Branch.Active != "" {
		activeBranch = cfg.Branch.Active
	}

	// Pre-restore hook: abort on non-zero exit.
	if cfg != nil && cfg.Hooks.PreRestore != "" {
		if err := hooks.Run(cfg.Hooks.PreRestore, projectRoot, snapshotID, activeBranch); err != nil {
			return nil, fmt.Errorf("pre-restore hook failed: %w", err)
		}
	}

	result, err := RestoreToDir(projectRoot, snapshotID, projectRoot)
	if err != nil {
		return nil, err
	}

	// Post-restore hook: non-fatal.
	if cfg != nil {
		hooks.RunPost(cfg.Hooks.PostRestore, projectRoot, snapshotID, activeBranch)
	}

	return result, nil
}

// RestoreToDir rolls a snapshot's file set back into targetDir.
// projectRoot is where .avc/ lives (object store + DB lookups).
// targetDir is where files are written — either the project root or a workspace.
// The two may differ when restoring into a branch workspace.
func RestoreToDir(projectRoot, snapshotID, targetDir string) (*Result, error) {
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

	// Build a set of relative paths present in the target snapshot.
	targetPaths := make(map[string]bool, len(targetFiles))
	for _, f := range targetFiles {
		targetPaths[f.RelativePath] = true
	}

	// Walk targetDir and remove any file not in the target snapshot.
	// This cleans up files added after the snapshot was taken.
	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".avc", ".git", ".hg", ".svn", ".bzr":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(targetDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !targetPaths[rel] {
			_ = os.Remove(path)
		}
		return nil
	})

	// Write back every file from the target snapshot.
	// For workspace restores, build a warm stat cache from the freshly written
	// files so that the first avc_snapshot on the branch is a stat-only pass
	// (no disk reads, no re-hashing of untouched files).
	isWorkspace := targetDir != projectRoot
	var wsCache *statcache.Cache
	if isWorkspace {
		wsCache = statcache.Empty()
	}

	var restoredSize int64
	for _, f := range targetFiles {
		absPath := filepath.Join(targetDir, filepath.FromSlash(f.RelativePath))

		data, err := readObject(projectRoot, f.FileHash)
		if err != nil {
			return nil, fmt.Errorf("read object for %s: %w", f.RelativePath, err)
		}
		if err := fileutil.WriteFile(absPath, data); err != nil {
			return nil, fmt.Errorf("write file %s: %w", f.RelativePath, err)
		}
		restoredSize += f.FileSize

		if isWorkspace {
			if info, err := os.Stat(absPath); err == nil {
				wsCache.Set(f.RelativePath, info, f.FileHash)
			}
		}
	}

	if isWorkspace {
		// Derive branch name from workspace path so we can key the cache file.
		workspacesBase := filepath.Join(projectRoot, ".avc", "workspaces")
		if rel, err := filepath.Rel(workspacesBase, targetDir); err == nil && !strings.HasPrefix(rel, "..") {
			cachePath := statcache.WorkspaceCachePath(projectRoot, rel)
			_ = wsCache.SaveToPath(cachePath)
		}
	} else {
		// Stat cache is only valid for the real project root — invalidate it after restore.
		statcache.Invalidate(projectRoot)
	}

	return &Result{
		SnapshotID:    snapshotID,
		RestoredFiles: len(targetFiles),
		RestoredSize:  restoredSize,
	}, nil
}

// objectPath returns the path inside .avc/objects/ where a file's content is stored.
func objectPath(projectRoot, hash string) string {
	return filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
}

// ReadObject reads a stored file blob by its hash. Exported for use by merge.
func ReadObject(projectRoot, hash string) ([]byte, error) {
	return readObject(projectRoot, hash)
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
