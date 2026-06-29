// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package snapshot handles creating and storing project snapshots.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/hooks"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// Result is returned by Create after a successful snapshot.
type Result struct {
	ID        string
	Label     string
	Timestamp int64
	AgentName string
	Notes     string
	FileCount int
	TotalSize int64
	BranchID  string
}

// Create walks sourceDir, hashes all tracked files, and persists a snapshot
// record to the database. projectRoot is where .avc/ lives; sourceDir is the
// directory to walk (pass "" to use projectRoot, which is the default for main).
// For branch workspaces, sourceDir is the workspace path and projectRoot is the
// real project root.
//
// branchID associates the snapshot with a branch; pass "" for unscoped snapshots.
func Create(projectRoot, label, agentName, notes, branchID, sourceDir string) (*Result, error) {
	if sourceDir == "" {
		sourceDir = projectRoot
	}

	// Load config for hooks and retention (best-effort; nil cfg means no hooks).
	cfg, _ := config.Load(projectRoot)

	// Determine active branch name for hook env var (best effort — empty on error).
	activeBranch := ""
	if cfg != nil {
		activeBranch = cfg.Branch.Active
		if activeBranch == "" {
			activeBranch = "main"
		}
	}

	// Pre-snapshot hook: abort if it exits non-zero.
	if cfg != nil && cfg.Hooks.PreSnapshot != "" {
		if err := hooks.Run(cfg.Hooks.PreSnapshot, projectRoot, "", activeBranch); err != nil {
			return nil, fmt.Errorf("pre-snapshot hook failed: %w", err)
		}
	}

	ignore, err := fileutil.LoadIgnoreRules(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load ignore rules: %w", err)
	}

	paths, err := fileutil.WalkProject(sourceDir, ignore)
	if err != nil {
		return nil, fmt.Errorf("walk project: %w", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized (run `avc init`): %w", err)
	}

	// Determine which stat cache to use.
	// For the project root, use the standard cache path.
	// For workspace directories, use a per-branch cache populated during workspace
	// materialization — this makes the first snapshot on a branch a stat-only pass.
	useCache := true
	var cachePath string
	workspacesBase := filepath.Join(projectRoot, ".avc", "workspaces")
	if sourceDir == projectRoot {
		cachePath = "" // signal to use statcache.Load / cache.Save below
	} else if rel, err := filepath.Rel(workspacesBase, sourceDir); err == nil && !strings.HasPrefix(rel, "..") {
		cachePath = statcache.WorkspaceCachePath(projectRoot, rel)
	} else {
		useCache = false
	}

	var cache *statcache.Cache
	if !useCache {
		cache = statcache.Empty()
	} else if cachePath == "" {
		cache, _ = statcache.Load(projectRoot)
	} else {
		cache, _ = statcache.LoadFromPath(cachePath)
	}

	snapID := newSnapID()
	now := time.Now().Unix()

	var totalSize int64
	files := make([]*db.File, 0, len(paths))

	for _, absPath := range paths {
		rel, _ := filepath.Rel(sourceDir, absPath)
		rel = filepath.ToSlash(rel)

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat file %s: %w", absPath, err)
		}

		var hash string
		var size int64

		if h, hit := cache.Lookup(rel, info); hit {
			// File unchanged since the last snapshot — object already stored.
			hash = h
			size = info.Size()
		} else {
			// File is new or modified — read once, derive hash from bytes.
			data, h, err := fileutil.ReadAndHash(absPath)
			if err != nil {
				return nil, fmt.Errorf("read file %s: %w", absPath, err)
			}
			if err := restore.StoreObject(projectRoot, h, data); err != nil {
				return nil, fmt.Errorf("store object %s: %w", absPath, err)
			}
			hash = h
			size = int64(len(data))
			cache.Set(rel, info, hash)
		}

		files = append(files, &db.File{
			ID:           newFileID(),
			SnapshotID:   snapID,
			RelativePath: rel,
			FileHash:     hash,
			FileSize:     size,
		})
		totalSize += size
	}

	snap := &db.Snapshot{
		ID:        snapID,
		ProjectID: project.ID,
		Timestamp: now,
		Label:     label,
		AgentName: agentName,
		Notes:     notes,
		FileCount: len(files),
		TotalSize: totalSize,
		BranchID:  branchID,
	}

	if err := store.InsertSnapshot(snap); err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}
	if err := store.InsertFilesBatch(files); err != nil {
		return nil, fmt.Errorf("insert files: %w", err)
	}

	// Persist updated cache — best-effort.
	if useCache {
		cache.SnapshotID = snapID
		if cachePath == "" {
			_ = cache.Save(projectRoot)
		} else {
			_ = cache.SaveToPath(cachePath)
		}
	}

	result := &Result{
		ID:        snapID,
		Label:     label,
		Timestamp: now,
		AgentName: agentName,
		Notes:     notes,
		FileCount: len(files),
		TotalSize: totalSize,
		BranchID:  branchID,
	}

	// Apply retention policy (best-effort).
	if branchID != "" && cfg != nil {
		_ = retention.Enforce(projectRoot, branchID, &cfg.Retention, os.Stderr)
	}

	// Post-snapshot hook: non-fatal (errors logged to stderr).
	if cfg != nil {
		hooks.RunPost(cfg.Hooks.PostSnapshot, projectRoot, result.ID, activeBranch)
	}

	return result, nil
}
