// Package branch manages AVC branches (agent workspaces).
package branch

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/statcache"
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

		data, hash, err := fileutil.ReadAndHash(absPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		dest := filepath.Join(ws, filepath.FromSlash(rel))
		if err := fileutil.WriteFile(dest, data); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
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
	if name == "main" {
		return nil, fmt.Errorf("'main' is a reserved branch name")
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

// List returns all branches for the project.
func List(projectRoot string) ([]*db.Branch, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized: %w", err)
	}
	return store.ListBranches(project.ID)
}

// Switch updates the active branch in config.
// Does not modify the working directory — use avc restore explicitly.
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
	return config.SetActiveBranch(projectRoot, name)
}

// Delete removes a branch record and its workspace directory.
// Refuses to delete main or the active branch.
// Snapshots on the branch are orphaned, not deleted.
func Delete(projectRoot, name string) error {
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
	if err := store.DeleteBranch(b.ID); err != nil {
		return err
	}

	return RemoveWorkspace(projectRoot, name)
}

// GetActiveBranchID returns the db.Branch.ID for the currently active branch.
func GetActiveBranchID(projectRoot string) (string, error) {
	cfg, err := config.Load(projectRoot)
	if err != nil {
		return "", err
	}
	activeName := cfg.Branch.Active
	if activeName == "" {
		activeName = "main"
	}

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
func GetActiveBranchName(projectRoot string) string {
	cfg, err := config.Load(projectRoot)
	if err != nil || cfg.Branch.Active == "" {
		return "main"
	}
	return cfg.Branch.Active
}

func newBranchID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "branch-" + hex.EncodeToString(b)
}
