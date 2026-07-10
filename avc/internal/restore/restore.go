// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package restore handles rolling back a project to a previous snapshot.
package restore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/hooks"
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

// objectPath returns the path inside .avc/objects/ where a file's content is stored.
func objectPath(projectRoot, hash string) string {
	if len(hash) < 3 {
		return ""
	}
	return filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
}

// ReadObject reads a stored file blob by its hash. Exported for use by merge.
func ReadObject(projectRoot, hash string) ([]byte, error) {
	return readObject(projectRoot, hash)
}

// readObject reads a stored file blob by its hash.
func readObject(projectRoot, hash string) ([]byte, error) {
	if len(hash) < 3 {
		return nil, fmt.Errorf("invalid object hash %q", hash)
	}
	path := objectPath(projectRoot, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("object %q not found: %w", hash, err)
	}
	return data, nil
}

// StoreObject writes file content to the object store under its hash.
// Called during snapshot creation to persist actual file bytes.
//
// Written atomically (temp file + rename) so a crash or disk-full mid-write
// can never leave a truncated blob on the final path — the existence check
// below would otherwise treat a torn write as "already stored" forever,
// permanently corrupting every future snapshot that dedupes against it.
func StoreObject(projectRoot, hash string, data []byte) error {
	if len(hash) < 3 {
		return fmt.Errorf("invalid object hash %q", hash)
	}
	path := objectPath(projectRoot, hash)
	if _, err := os.Stat(path); err == nil {
		return nil // already stored (content-addressed deduplication)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// The suffix must be unique per call, not just per process: concurrent
	// goroutines (e.g. two agents snapshotting identical new content at once)
	// share a PID, so a PID-only suffix would collide and race on Windows,
	// where a file mid-rename cannot be opened by a second writer.
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	tmp := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		os.Remove(tmp)
		return err
	}

	// Two concurrent StoreObject calls for the same hash are guaranteed to be
	// writing byte-identical content (that's what content-addressing means),
	// but Windows does not make concurrent renames onto the same destination
	// as forgiving as POSIX rename(2) — a losing rename can surface as
	// "access is denied" even though the winner's result is fine. Retry a
	// few times against a destination that may only just now have appeared.
	var renameErr error
	for attempt := 0; attempt < 5; attempt++ {
		if renameErr = os.Rename(tmp, path); renameErr == nil {
			return nil
		}
		if _, statErr := os.Stat(path); statErr == nil {
			os.Remove(tmp)
			return nil // a concurrent writer already stored identical content
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	os.Remove(tmp)
	return renameErr
}
