// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package restore handles rolling back a project to a previous snapshot.
package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/hooks"
	"github.com/trevarix/agentic-vc/avc/internal/objstore"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
	"github.com/trevarix/agentic-vc/avc/internal/trash"
)

// trashAutoEmptyAge is how long quarantined files sit in .avc/trash/ before a
// restore opportunistically sweeps them away. Best-effort — never blocks restore.
const trashAutoEmptyAge = 7 * 24 * time.Hour

// Result is returned by Restore on success.
type Result struct {
	SnapshotID       string
	RestoredFiles    int
	RestoredSize     int64
	QuarantinedFiles int    // untracked files moved to trash instead of deleted
	TrashOpID        string // trash session ID; "" if nothing was quarantined
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

	// Ignored files (.env, node_modules/, local DBs, ...) are by definition
	// never captured in a snapshot. They must never be touched by the
	// deletion sweep below, or every restore would delete them.
	ignore, err := fileutil.LoadIgnoreRules(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load ignore rules: %w", err)
	}

	// Untracked-but-not-ignored files are quarantined rather than deleted —
	// defense in depth so a restore can never destroy data irrecoverably.
	session := trash.NewSession(projectRoot, "restore")
	quarantined := 0

	// Walk targetDir and quarantine any file not in the target snapshot.
	// This cleans up files added after the snapshot was taken.
	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(targetDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			switch d.Name() {
			case ".avc", ".git", ".hg", ".svn", ".bzr":
				return filepath.SkipDir
			}
			if ignore.MatchesDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if ignore.Matches(rel) {
			return nil // ignored file — never touch it
		}
		if !targetPaths[rel] {
			if err := session.Move(targetDir, rel); err == nil {
				quarantined++
			}
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
		// Restore the recorded permission bits (notably the executable bit).
		// f.FileMode is 0 for rows written before mode tracking existed —
		// WriteFile's own default (0644) already applies in that case.
		if f.FileMode != 0 {
			_ = os.Chmod(absPath, os.FileMode(f.FileMode))
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

	// Quarantining files can leave their now-empty parent directories behind
	// (e.g. a workspace subdirectory whose only file was untracked).
	removeEmptyDirs(targetDir)

	// Opportunistically sweep trash entries older than the retention window.
	// Best-effort — a failure here must never fail the restore itself.
	if removed, err := trash.Empty(projectRoot, trashAutoEmptyAge); err == nil && removed > 0 {
		fmt.Fprintf(os.Stderr, "[avc] Cleared %d trash entr(ies) older than %s\n", removed, trashAutoEmptyAge)
	}

	return &Result{
		SnapshotID:       snapshotID,
		RestoredFiles:    len(targetFiles),
		RestoredSize:     restoredSize,
		QuarantinedFiles: quarantined,
		TrashOpID:        session.OpID(),
	}, nil
}

// ReadObject reads a stored file blob by its hash. Exported for use by merge.
// Thin wrapper over the objstore package, which owns the on-disk format.
func ReadObject(projectRoot, hash string) ([]byte, error) {
	return objstore.Read(projectRoot, hash)
}

// readObject reads a stored file blob by its hash.
func readObject(projectRoot, hash string) ([]byte, error) {
	return objstore.Read(projectRoot, hash)
}

// StoreObject writes file content to the object store under its hash.
// Called during snapshot creation to persist actual file bytes. Thin wrapper
// over the objstore package, which owns the on-disk format (atomic writes,
// transparent zstd compression, legacy raw objects).
func StoreObject(projectRoot, hash string, data []byte) error {
	return objstore.Store(projectRoot, hash, data)
}

// removeEmptyDirs removes every directory under root that ends up empty,
// deepest first so a parent that becomes empty only after its child is
// removed is caught in the same pass. Used after a restore's deletion sweep
// so quarantining files doesn't leave orphaned empty directories behind.
// Best-effort: a directory that can't be removed is simply left in place.
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		switch d.Name() {
		case ".avc", ".git", ".hg", ".svn", ".bzr":
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			os.Remove(dir) //nolint:errcheck
		}
	}
}
