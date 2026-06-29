// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package branch manages AVC branches (agent workspaces).
package branch

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

const workspacesDir = "workspaces"

// WorkspacePath returns the absolute path of the materialized workspace for a
// branch. Returns "" for main — main always uses the real project root.
func WorkspacePath(projectRoot, branchName string) string {
	if branchName == "" || branchName == "main" {
		return ""
	}
	return filepath.Join(projectRoot, ".avc", workspacesDir, branchName)
}

// MaterializeWorkspace populates the workspace directory for a branch.
// If the branch has a base snapshot, files are restored from the object store.
// If there is no base snapshot (first branch on a new project), files are copied
// directly from the project root and a warm stat cache is written so that the
// first avc_snapshot on the branch is a stat-only pass.
func MaterializeWorkspace(projectRoot string, b *db.Branch) error {
	ws := WorkspacePath(projectRoot, b.Name)
	if ws == "" {
		return nil // main has no workspace
	}
	if err := os.MkdirAll(ws, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	if b.BaseSnapshotID != "" {
		if _, err := restore.RestoreToDir(projectRoot, b.BaseSnapshotID, ws); err != nil {
			return fmt.Errorf("materialize workspace: %w", err)
		}
		return nil
	}
	// No base snapshot — copy project root files directly into the workspace.
	return copyToWorkspace(projectRoot, ws, b.Name)
}

// copyToWorkspace copies all tracked files from projectRoot into ws and writes
// a warm stat cache so the first snapshot on the branch skips re-hashing.
//
// It uses copyFileOptimized which tries (in order): hardlink → regular copy.
// Hardlinks are zero-cost until a file is mutated, making branch creation on
// the same filesystem nearly instant for large projects.
// If hardlinking fails (cross-device, unsupported FS), it falls back to a
// byte-for-byte copy transparently.
//
// Note: when a file is hardlinked, writes in the workspace will mutate the
// source file too until the OS breaks the link on write (copy-on-write
// semantics). Because AVC workspaces are written by avc_restore (which always
// creates a new file), this is safe in practice.
func copyToWorkspace(projectRoot, ws, branchName string) error {
	ignore, err := fileutil.LoadIgnoreRules(projectRoot)
	if err != nil {
		return fmt.Errorf("load ignore rules: %w", err)
	}
	paths, err := fileutil.WalkProject(projectRoot, ignore)
	if err != nil {
		return fmt.Errorf("walk project: %w", err)
	}

	cache := statcache.Empty()
	for _, absPath := range paths {
		rel, _ := filepath.Rel(projectRoot, absPath)
		rel = filepath.ToSlash(rel)

		dest := filepath.Join(ws, filepath.FromSlash(rel))
		if err := copyFileOptimized(absPath, dest); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		// Hash the file for the stat cache. We read after copy so that the
		// stat-cache entry matches the actual inode on disk.
		_, hash, err := fileutil.ReadAndHash(dest)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		if info, err := os.Stat(dest); err == nil {
			cache.Set(rel, info, hash)
		}
	}

	_ = cache.SaveToPath(statcache.WorkspaceCachePath(projectRoot, branchName))
	return nil
}

// RemoveWorkspace deletes the workspace directory and its stat cache for a branch.
// No-op if the workspace does not exist.
func RemoveWorkspace(projectRoot, branchName string) error {
	ws := WorkspacePath(projectRoot, branchName)
	if ws == "" {
		return nil
	}
	if err := os.RemoveAll(ws); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove workspace: %w", err)
	}
	cachePath := statcache.WorkspaceCachePath(projectRoot, branchName)
	_ = os.Remove(cachePath)
	return nil
}

// Create creates a new branch rooted at baseSnapshotID and materializes its
// workspace directory. If baseSnapshotID is empty, the HEAD snapshot of main
// is used. Branch creation never takes a new snapshot — it inherits the base
// by reference only.
func Create(projectRoot, name, baseSnapshotID string) (*db.Branch, error) {
	if err := ValidateBranchName(name); err != nil {
		return nil, err
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized: %w", err)
	}

	if _, err := store.GetBranchByName(project.ID, name); err == nil {
		return nil, fmt.Errorf("branch '%s' already exists", name)
	}

	if baseSnapshotID == "" {
		mainBranch, err := store.GetBranchByName(project.ID, "main")
		if err != nil {
			return nil, fmt.Errorf("main branch not found: %w", err)
		}
		if head, err := store.GetHeadSnapshot(mainBranch.ID); err == nil {
			baseSnapshotID = head.ID
		}
		// If main has no snapshots, leave baseSnapshotID = "" and materialize
		// the workspace directly from the project root files.
	} else {
		if _, err := store.GetSnapshot(baseSnapshotID); err != nil {
			return nil, fmt.Errorf("snapshot '%s' not found", baseSnapshotID)
		}
	}

	b := &db.Branch{
		ID:             newBranchID(),
		Name:           name,
		ProjectID:      project.ID,
		BaseSnapshotID: baseSnapshotID,
		CreatedAt:      time.Now().Unix(),
		Status:         "active",
	}
	if err := store.InsertBranch(b); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	// Materialize the workspace outside the DB transaction — failures here are
	// surfaced to the caller but do not roll back the branch record.
	if err := MaterializeWorkspace(projectRoot, b); err != nil {
		return nil, err
	}

	return b, nil
}

// List returns branches for the project filtered by status.
// Pass status="" to return all branches regardless of status.
// Pass status="active" (the default for most callers) to exclude merged/abandoned.
func List(projectRoot string) ([]*db.Branch, error) {
	return ListByStatus(projectRoot, "active")
}

// ListByStatus returns branches filtered by the given status string.
// Pass "" to return all statuses.
func ListByStatus(projectRoot, status string) ([]*db.Branch, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized: %w", err)
	}
	return store.ListBranchesByStatus(project.ID, status)
}

// Switch updates the active branch in both the DB (project_state) and
// config.toml. Does not modify the working directory.
func Switch(projectRoot, name string) error {
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return fmt.Errorf("project not initialized: %w", err)
	}
	if _, err := store.GetBranchByName(project.ID, name); err != nil {
		return fmt.Errorf("branch '%s' not found", name)
	}
	// Write to DB (authoritative) and config.toml (backwards compat).
	if err := store.SetActiveBranch(project.ID, name); err != nil {
		return fmt.Errorf("set active branch in db: %w", err)
	}
	return config.SetActiveBranch(projectRoot, name)
}

// Delete removes a branch record and its workspace directory.
// Refuses to delete main or the active branch.
// When keepHistory is false (the default), all snapshots on the branch are
// deleted from the DB in the same operation. Pass keepHistory = true to
// retain the snapshot rows (they become unreachable but can be inspected via
// avc log until the next `avc gc --run` sweeps the object store).
func Delete(projectRoot, name string, keepHistory bool) error {
	if name == "main" {
		return fmt.Errorf("cannot delete the main branch")
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return err
	}
	if cfg.Branch.Active == name {
		return fmt.Errorf("cannot delete the active branch; switch to another branch first")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return fmt.Errorf("project not initialized: %w", err)
	}
	b, err := store.GetBranchByName(project.ID, name)
	if err != nil {
		return fmt.Errorf("branch '%s' not found", name)
	}

	// Prepare snapshot rows for branch deletion.
	// • keepHistory=false (default): delete all snapshot and file rows for the
	//   branch so the branch record can be deleted without FK violations, and the
	//   unreferenced blobs become candidates for `avc gc`.
	// • keepHistory=true: NULL-out branch_id on the snapshot rows so they are
	//   detached (and therefore retained in the DB) while still satisfying the
	//   FK constraint on branches(id).
	if keepHistory {
		if err := store.DetachSnapshotsFromBranch(b.ID); err != nil {
			return fmt.Errorf("detach branch snapshots: %w", err)
		}
	} else {
		if err := store.DeleteSnapshotsByBranch(b.ID); err != nil {
			return fmt.Errorf("delete branch snapshots: %w", err)
		}
	}

	if err := store.DeleteBranch(b.ID); err != nil {
		return err
	}

	return RemoveWorkspace(projectRoot, name)
}

// GetActiveBranchID returns the db.Branch.ID for the currently active branch.
func GetActiveBranchID(projectRoot string) (string, error) {
	activeName := GetActiveBranchName(projectRoot)

	store, err := db.Open(projectRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return "", fmt.Errorf("project not initialized: %w", err)
	}
	b, err := store.GetBranchByName(project.ID, activeName)
	if err != nil {
		return "", fmt.Errorf("active branch '%s' not found; run `avc init` to set up branches", activeName)
	}
	return b.ID, nil
}

// GetActiveBranchName returns the name of the currently active branch.
// It first checks the project_state DB table; falls back to config.toml for
// backwards compatibility with projects created before Phase 7.
func GetActiveBranchName(projectRoot string) string {
	store, err := db.Open(projectRoot)
	if err == nil {
		defer store.Close()
		proj, projErr := store.GetProject(projectRoot)
		if projErr == nil {
			if name, dbErr := store.GetActiveBranch(proj.ID); dbErr == nil && name != "" {
				return name
			}
		}
	}
	// Fall back to config.toml.
	cfg, cfgErr := config.Load(projectRoot)
	if cfgErr != nil || cfg.Branch.Active == "" {
		return "main"
	}
	return cfg.Branch.Active
}

// Rename renames a branch: updates the DB record, renames the workspace
// directory, and updates the active branch reference if it was the active branch.
func Rename(projectRoot, oldName, newName string) error {
	if oldName == "main" {
		return fmt.Errorf("cannot rename the main branch")
	}
	if err := ValidateBranchName(newName); err != nil {
		return err
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return fmt.Errorf("project not initialized: %w", err)
	}

	b, err := store.GetBranchByName(proj.ID, oldName)
	if err != nil {
		return fmt.Errorf("branch '%s' not found", oldName)
	}

	if _, err := store.GetBranchByName(proj.ID, newName); err == nil {
		return fmt.Errorf("branch '%s' already exists", newName)
	}

	// Rename workspace directory first (reversible if DB update fails).
	oldWS := WorkspacePath(projectRoot, oldName)
	newWS := WorkspacePath(projectRoot, newName)
	if _, statErr := os.Stat(oldWS); statErr == nil {
		if err := os.Rename(oldWS, newWS); err != nil {
			return fmt.Errorf("rename workspace: %w", err)
		}
	}

	// Also rename the stat cache file.
	oldCache := statcache.WorkspaceCachePath(projectRoot, oldName)
	newCache := statcache.WorkspaceCachePath(projectRoot, newName)
	_ = os.Rename(oldCache, newCache)

	// Update DB.
	if err := store.RenameBranch(b.ID, newName); err != nil {
		// Rollback workspace rename.
		if _, statErr := os.Stat(newWS); statErr == nil {
			os.Rename(newWS, oldWS)
			os.Rename(newCache, oldCache)
		}
		return fmt.Errorf("rename branch in db: %w", err)
	}

	// Update active branch references if this was the active branch.
	if GetActiveBranchName(projectRoot) == oldName {
		_ = store.SetActiveBranch(proj.ID, newName)
		_ = config.SetActiveBranch(projectRoot, newName)
	}
	return nil
}

// Abandon marks a branch as abandoned without removing any data.
func Abandon(projectRoot, name string) error {
	if name == "main" {
		return fmt.Errorf("cannot abandon the main branch")
	}
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return fmt.Errorf("project not initialized: %w", err)
	}
	b, err := store.GetBranchByName(proj.ID, name)
	if err != nil {
		return fmt.Errorf("branch '%s' not found", name)
	}
	return store.SetBranchStatus(b.ID, "abandoned")
}

// PruneMergedWorkspaces removes workspace directories for all branches whose
// status is "merged". DB records and snapshots are retained.
func PruneMergedWorkspaces(projectRoot string) ([]string, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized: %w", err)
	}
	branches, err := store.ListBranchesByStatus(proj.ID, "merged")
	if err != nil {
		return nil, err
	}

	var pruned []string
	for _, b := range branches {
		ws := WorkspacePath(projectRoot, b.Name)
		if ws == "" {
			continue
		}
		if _, statErr := os.Stat(ws); os.IsNotExist(statErr) {
			continue // already gone
		}
		if err := RemoveWorkspace(projectRoot, b.Name); err != nil {
			return pruned, fmt.Errorf("prune workspace for '%s': %w", b.Name, err)
		}
		pruned = append(pruned, b.Name)
	}
	return pruned, nil
}

func newBranchID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "branch-" + hex.EncodeToString(b)
}
