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
	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/hooks"
	"github.com/trevarix/agentic-vc/avc/internal/objstore"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// Result is returned by Create after a successful snapshot.
type Result struct {
	ID           string
	Label        string
	Timestamp    int64
	AgentName    string
	Notes        string
	FileCount    int
	TotalSize    int64
	BranchID     string
	SessionID    string
	Task         string
	Summary      string   // heuristic one-line change summary vs the previous branch HEAD; empty when no baseline exists
	SkippedLarge []string // relative paths skipped for exceeding the max file size
	NewFiles     int      // files tracked here but absent from the previous snapshot (all files when no baseline)
	CarriedFiles int      // previously-tracked files now ignore-matched but kept because they still exist on disk
}

// Options describes one snapshot to create. Label is required; everything
// else is optional. SourceDir "" means walk projectRoot (the default for
// main); for branch workspaces it is the workspace path.
type Options struct {
	Label     string
	AgentName string
	Notes     string
	BranchID  string // associates the snapshot with a branch; "" for unscoped snapshots
	SourceDir string
	SessionID string // agent session attribution (see `avc timeline`)
	Task      string // one-line task description for the session
}

// Create walks sourceDir, hashes all tracked files, and persists a snapshot
// record to the database. projectRoot is where .avc/ lives; sourceDir is the
// directory to walk (pass "" to use projectRoot, which is the default for main).
// For branch workspaces, sourceDir is the workspace path and projectRoot is the
// real project root.
//
// branchID associates the snapshot with a branch; pass "" for unscoped snapshots.
func Create(projectRoot, label, agentName, notes, branchID, sourceDir string) (*Result, error) {
	return CreateWithOptions(projectRoot, Options{
		Label:     label,
		AgentName: agentName,
		Notes:     notes,
		BranchID:  branchID,
		SourceDir: sourceDir,
	})
}

// CreateWithOptions is Create with session attribution (session_id/task).
func CreateWithOptions(projectRoot string, opts Options) (*Result, error) {
	label, agentName, notes := opts.Label, opts.AgentName, opts.Notes
	branchID, sourceDir := opts.BranchID, opts.SourceDir
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

	ignore, err := loadIgnoreRulesForSource(projectRoot, sourceDir)
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

	maxFileSizeMB := config.DefaultMaxFileSizeMB
	if cfg != nil && cfg.Snapshot.MaxFileSizeMB > 0 {
		maxFileSizeMB = cfg.Snapshot.MaxFileSizeMB
	}
	maxFileSizeBytes := int64(maxFileSizeMB) * 1024 * 1024

	var totalSize int64
	var skippedLarge []string
	files := make([]*db.File, 0, len(paths))
	tracked := make(map[string]bool, len(paths)) // every rel path we've decided on

	// addFile hashes one existing file (info from a prior Stat) and appends it
	// to the snapshot, or records it as skipped when it exceeds the size cap.
	addFile := func(absPath, rel string, info os.FileInfo) error {
		tracked[rel] = true

		// Files larger than the configured cap are skipped entirely (not
		// read, not hashed, not stored) rather than risking an
		// out-of-memory read on an accidentally-tracked large binary.
		if info.Size() > maxFileSizeBytes {
			skippedLarge = append(skippedLarge, rel)
			fmt.Fprintf(os.Stderr,
				"[avc] warning: skipping %s (%.1f MB exceeds the %d MB snapshot limit; set [snapshot] max_file_size_mb in .avc/config.toml to change this)\n",
				rel, float64(info.Size())/(1024*1024), maxFileSizeMB,
			)
			return nil
		}

		var hash string
		var size int64
		// A cache hit is only trusted when the object it points to actually
		// exists — a stale or corrupted cache must never produce a snapshot
		// that references content the store doesn't hold.
		if h, hit := cache.Lookup(rel, info); hit && objstore.Exists(projectRoot, h) {
			hash = h
			size = info.Size()
		} else {
			// File is new or modified — read once, derive hash from bytes.
			data, h, err := fileutil.ReadAndHash(absPath)
			if err != nil {
				return fmt.Errorf("read file %s: %w", absPath, err)
			}
			if err := restore.StoreObject(projectRoot, h, data); err != nil {
				return fmt.Errorf("store object %s: %w", absPath, err)
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
			FileMode:     uint32(info.Mode().Perm()),
		})
		totalSize += size
		return nil
	}

	for _, absPath := range paths {
		rel, _ := filepath.Rel(sourceDir, absPath)
		rel = filepath.ToSlash(rel)

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat file %s: %w", absPath, err)
		}
		if err := addFile(absPath, rel, info); err != nil {
			return nil, err
		}
	}

	// Load the previous snapshot's files once — the baseline for carry-forward,
	// the new-file count, and the change summary. Empty when there's none.
	summaryBaseID := previousSnapshotID(store, branchID)
	var prevFiles []*db.File
	if summaryBaseID != "" {
		if prevFiles, err = store.GetSnapshotFiles(summaryBaseID); err != nil {
			return nil, err
		}
	}

	// Untrack-vs-delete: an ignore rule must never untrack a file that is still
	// present on disk (git's rule — .gitignore does not untrack tracked files;
	// `git rm --cached` does). Without this, adding a path to .avcignore
	// mid-branch drops its already-tracked files from the snapshot, a later
	// branch diff reports them [deleted], and a merge would delete the real
	// files from the project root. So carry forward, with current content, any
	// previously-tracked file the walk skipped (now ignored) that still exists.
	carried, err := carryForwardTrackedFiles(prevFiles, sourceDir, tracked, addFile)
	if err != nil {
		return nil, err
	}
	if carried > 0 {
		fmt.Fprintf(os.Stderr,
			"[avc] note: %d previously-tracked file(s) now match an ignore rule but still exist on disk — kept in the snapshot (ignoring does not untrack; delete the files or use an explicit untrack to stop tracking)\n",
			carried,
		)
	}

	// New-file count: tracked files absent from the previous snapshot. All
	// files are new when there is no baseline. Surfaced so an agent notices an
	// unexpected spike (e.g. test output entering tracking).
	newFiles := countNewFiles(files, prevFiles)

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
		SessionID: opts.SessionID,
		Task:      opts.Task,
	}

	if err := store.InsertSnapshotWithFiles(snap, files); err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}

	// Generate and cache the change summary vs the baseline. Best-effort: a
	// snapshot that succeeded must not fail because its summary could not be
	// computed, and `avc timeline` recomputes missing summaries lazily.
	summary := ""
	if summaryBaseID != "" {
		if diffFiles, sumErr := diffpkg.CacheSummaries(projectRoot, summaryBaseID, snapID); sumErr == nil {
			summary = diffpkg.Summarize(diffFiles)
		}
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
		ID:           snapID,
		Label:        label,
		Timestamp:    now,
		AgentName:    agentName,
		Notes:        notes,
		FileCount:    len(files),
		TotalSize:    totalSize,
		BranchID:     branchID,
		SessionID:    opts.SessionID,
		Task:         opts.Task,
		Summary:      summary,
		SkippedLarge: skippedLarge,
		NewFiles:     newFiles,
		CarriedFiles: carried,
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

// carryForwardTrackedFiles re-adds files that were tracked in prevFiles (the
// branch's previous snapshot) but were skipped by the current walk (now
// matched by an ignore rule) and still exist on disk. It calls addFile for
// each so the current content is captured, and returns how many were carried
// forward. A file that is genuinely gone from disk is not carried — it is a
// real deletion. With no prior snapshot, prevFiles is empty and it returns 0.
func carryForwardTrackedFiles(
	prevFiles []*db.File,
	sourceDir string,
	tracked map[string]bool,
	addFile func(absPath, rel string, info os.FileInfo) error,
) (int, error) {
	carried := 0
	for _, f := range prevFiles {
		if tracked[f.RelativePath] {
			continue // already handled by the walk (or intentionally skipped)
		}
		absPath := filepath.Join(sourceDir, filepath.FromSlash(f.RelativePath))
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.IsDir() {
			continue // genuinely deleted (or replaced by a dir) — a real removal
		}
		if err := addFile(absPath, f.RelativePath, info); err != nil {
			return carried, err
		}
		carried++
	}
	return carried, nil
}

// countNewFiles returns how many entries in files have a relative path not
// present in prevFiles. With no baseline (empty prevFiles) every file is new.
func countNewFiles(files, prevFiles []*db.File) int {
	prev := make(map[string]bool, len(prevFiles))
	for _, f := range prevFiles {
		prev[f.RelativePath] = true
	}
	n := 0
	for _, f := range files {
		if !prev[f.RelativePath] {
			n++
		}
	}
	return n
}

// previousSnapshotID returns the branch's current HEAD (the state before the
// snapshot in progress), falling back to the branch's base snapshot for the
// first snapshot on a branch. Empty when there is no baseline.
func previousSnapshotID(store *db.Store, branchID string) string {
	if branchID == "" {
		return ""
	}
	if head, err := store.GetHeadSnapshot(branchID); err == nil {
		return head.ID
	}
	if b, err := store.GetBranchByID(branchID); err == nil {
		return b.BaseSnapshotID
	}
	return ""
}

// loadIgnoreRulesForSource builds the ignore rules for a snapshot walk: the
// root .avcignore layered underneath the branch workspace's own. See
// fileutil.LoadLayeredIgnoreRules for the layering semantics — root rules are
// read fresh and always apply, so a root edit reaches live branches, while
// workspace-specific additions still apply. Layering is additive, so relaxing
// a pattern already in the workspace copy needs that copy updated too.
func loadIgnoreRulesForSource(projectRoot, sourceDir string) (*fileutil.IgnoreRules, error) {
	return fileutil.LoadLayeredIgnoreRules(projectRoot, sourceDir)
}
