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
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
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

// MaterializeWorkspace populates the workspace directory from the branch's base
// snapshot. Files are written from the object store into the workspace directory.
// If the workspace already exists it is overwritten (idempotent).
func MaterializeWorkspace(projectRoot string, b *db.Branch) error {
	if b.BaseSnapshotID == "" {
		return nil // nothing to materialize
	}
	ws := WorkspacePath(projectRoot, b.Name)
	if ws == "" {
		return nil // main has no workspace
	}
	if err := os.MkdirAll(ws, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	if _, err := restore.RestoreToDir(projectRoot, b.BaseSnapshotID, ws); err != nil {
		return fmt.Errorf("materialize workspace: %w", err)
	}
	return nil
}

// RemoveWorkspace deletes the workspace directory for a branch. No-op if the
// workspace does not exist.
func RemoveWorkspace(projectRoot, branchName string) error {
	ws := WorkspacePath(projectRoot, branchName)
	if ws == "" {
		return nil
	}
	if err := os.RemoveAll(ws); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove workspace: %w", err)
	}
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
		head, err := store.GetHeadSnapshot(mainBranch.ID)
		if err != nil {
			return nil, fmt.Errorf("main has no snapshots to branch from; create a snapshot first")
		}
		baseSnapshotID = head.ID
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
