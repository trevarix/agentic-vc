// Package api provides typed operation wrappers used by both the CLI and the
// web server.  All business logic lives in the internal sub-packages (branch,
// merge, snapshot, diff, …); this layer is a thin coordination point that
// prevents the CLI and web-server from duplicating the same call sequences.
package api

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	branchpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	diffpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
	mergepkg "github.com/SkillMythOrg/agentic-vc/avc/internal/merge"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

// ─── Snapshot operations ──────────────────────────────────────────────────────

// SnapshotOps groups snapshot operations for a project root.
type SnapshotOps struct{ ProjectRoot string }

// List returns snapshots matching the filter.
func (o SnapshotOps) List(f db.SnapshotFilter) ([]*db.Snapshot, error) {
	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSnapshotsFiltered(f)
}

// Create creates a new snapshot on the active branch (workspace-aware).
func (o SnapshotOps) Create(label, agent, notes string) (*snapshot.Result, error) {
	branchID, err := branchpkg.GetActiveBranchID(o.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("could not determine active branch: %w", err)
	}
	branchName := branchpkg.GetActiveBranchName(o.ProjectRoot)
	sourceDir := branchpkg.WorkspacePath(o.ProjectRoot, branchName)
	return snapshot.Create(o.ProjectRoot, label, agent, notes, branchID, sourceDir)
}

// Info returns a snapshot and its tracked files.
func (o SnapshotOps) Info(id string) (*db.Snapshot, []*db.File, error) {
	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return nil, nil, err
	}
	defer store.Close()
	snap, err := store.GetSnapshot(id)
	if err != nil {
		return nil, nil, err
	}
	files, err := store.GetSnapshotFiles(id)
	return snap, files, err
}

// Delete removes a snapshot and its file records.
func (o SnapshotOps) Delete(id string) error {
	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.GetSnapshot(id); err != nil {
		return fmt.Errorf("snapshot '%s' not found", id)
	}
	return store.DeleteSnapshot(id)
}

// Tag applies a tag to a snapshot (idempotent).
func (o SnapshotOps) Tag(snapshotID, tag string) error {
	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return fmt.Errorf("snapshot '%s' not found", snapshotID)
	}
	return store.TagSnapshot(snapshotID, tag)
}

// Untag removes a tag from a snapshot.
func (o SnapshotOps) Untag(snapshotID, tag string) error {
	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.UntagSnapshot(snapshotID, tag)
}

// RestoreFile restores one file from a snapshot, writing to the workspace on
// non-main branches.
func (o SnapshotOps) RestoreFile(snapshotID, filePath string) (*restore.RestoreFileResult, error) {
	targetDir := o.ProjectRoot
	branchName := branchpkg.GetActiveBranchName(o.ProjectRoot)
	if ws := branchpkg.WorkspacePath(o.ProjectRoot, branchName); ws != "" {
		targetDir = ws
	}
	return restore.RestoreFileToDir(o.ProjectRoot, snapshotID, filePath, targetDir)
}

// ─── Branch operations ────────────────────────────────────────────────────────

// BranchOps groups branch operations for a project root.
type BranchOps struct{ ProjectRoot string }

// BranchListResult holds the branch list and the active branch name.
type BranchListResult struct {
	Branches   []*db.Branch
	ActiveName string
}

// List returns active branches and the currently active branch name.
func (o BranchOps) List() (*BranchListResult, error) {
	branches, err := branchpkg.List(o.ProjectRoot)
	if err != nil {
		return nil, err
	}
	active := branchpkg.GetActiveBranchName(o.ProjectRoot)
	return &BranchListResult{Branches: branches, ActiveName: active}, nil
}

// ListAll returns all branches regardless of status.
func (o BranchOps) ListAll() (*BranchListResult, error) {
	branches, err := branchpkg.ListByStatus(o.ProjectRoot, "")
	if err != nil {
		return nil, err
	}
	active := branchpkg.GetActiveBranchName(o.ProjectRoot)
	return &BranchListResult{Branches: branches, ActiveName: active}, nil
}

// Create creates a new branch, switches to it, and returns the branch + workspace path.
func (o BranchOps) Create(name, fromSnapshotID string) (*db.Branch, string, error) {
	b, err := branchpkg.Create(o.ProjectRoot, name, fromSnapshotID)
	if err != nil {
		return nil, "", err
	}
	if err := branchpkg.Switch(o.ProjectRoot, name); err != nil {
		return nil, "", fmt.Errorf("branch created but auto-switch failed: %w", err)
	}
	ws := branchpkg.WorkspacePath(o.ProjectRoot, b.Name)
	return b, ws, nil
}

// Switch sets the active branch.
func (o BranchOps) Switch(name string) error {
	return branchpkg.Switch(o.ProjectRoot, name)
}

// Delete removes a branch and optionally its snapshots.
func (o BranchOps) Delete(name string, keepHistory bool) error {
	return branchpkg.Delete(o.ProjectRoot, name, keepHistory)
}

// BranchDiffResult holds a branch's cumulative diff.
type BranchDiffResult struct {
	BranchName     string
	FromSnapshotID string
	ToSnapshotID   string
	Diff           *diffpkg.Result
}

// Diff returns the cumulative diff from a branch's base snapshot to its HEAD.
func (o BranchOps) Diff(name string) (*BranchDiffResult, error) {
	branches, err := branchpkg.List(o.ProjectRoot)
	if err != nil {
		return nil, err
	}
	var branchID, baseSnapshotID string
	for _, b := range branches {
		if b.Name == name {
			branchID = b.ID
			baseSnapshotID = b.BaseSnapshotID
			break
		}
	}
	if branchID == "" {
		return nil, fmt.Errorf("branch '%s' not found", name)
	}

	store, err := db.Open(o.ProjectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if baseSnapshotID == "" {
		oldest, err := store.GetOldestSnapshot(branchID)
		if err != nil {
			return nil, fmt.Errorf("branch '%s' has no snapshots", name)
		}
		baseSnapshotID = oldest.ID
	}
	head, err := store.GetHeadSnapshot(branchID)
	if err != nil {
		return nil, fmt.Errorf("branch '%s' has no snapshots", name)
	}
	store.Close()

	result, err := diffpkg.Compare(o.ProjectRoot, baseSnapshotID, head.ID)
	if err != nil {
		return nil, err
	}
	return &BranchDiffResult{
		BranchName:     name,
		FromSnapshotID: baseSnapshotID,
		ToSnapshotID:   head.ID,
		Diff:           result,
	}, nil
}

// ─── Merge operations ─────────────────────────────────────────────────────────

// MergeOps groups merge operations for a project root.
type MergeOps struct{ ProjectRoot string }

// Preview returns the merge plan without writing any files.
func (o MergeOps) Preview(branchName string) (*mergepkg.Result, error) {
	return mergepkg.Preview(o.ProjectRoot, branchName)
}

// Merge executes the merge. Checks for conflicts first; if any exist, returns
// them without writing anything.
func (o MergeOps) Merge(branchName string) (*mergepkg.Result, error) {
	plan, err := mergepkg.Preview(o.ProjectRoot, branchName)
	if err != nil {
		return nil, err
	}
	if plan.Conflicts > 0 {
		return plan, nil // caller checks result.Conflicts
	}
	return mergepkg.Merge(o.ProjectRoot, branchName)
}

// Abort rolls back the last in-progress or conflicted merge.
func (o MergeOps) Abort() error {
	return mergepkg.Abort(o.ProjectRoot)
}

// ─── Status ───────────────────────────────────────────────────────────────────

// StatusResult holds the current working-tree status.
type StatusResult struct {
	BranchName    string
	SnapshotID    string
	SnapshotLabel string
	Files         []*diffpkg.FileDiff
}

// GetStatus compares the working tree to the last snapshot on the active branch.
func GetStatus(projectRoot string) (*StatusResult, error) {
	branchName := branchpkg.GetActiveBranchName(projectRoot)
	sourceDir := branchpkg.WorkspacePath(projectRoot, branchName)
	if sourceDir == "" {
		sourceDir = projectRoot
	}

	branchID, err := branchpkg.GetActiveBranchID(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("could not determine active branch: %w", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	head, headErr := store.GetHeadSnapshot(branchID)
	store.Close()

	if headErr != nil {
		return &StatusResult{BranchName: branchName}, nil
	}

	result, err := diffpkg.CompareWithCurrentDir(projectRoot, sourceDir, head.ID)
	if err != nil {
		return nil, err
	}
	return &StatusResult{
		BranchName:    branchName,
		SnapshotID:    head.ID,
		SnapshotLabel: head.Label,
		Files:         result.Files,
	}, nil
}

// ─── Storage ──────────────────────────────────────────────────────────────────

// StorageResult holds disk-usage information for a project.
type StorageResult struct {
	ProjectName     string              `json:"project_name"`
	DatabaseBytes   int64               `json:"database_bytes"`
	ObjectsBytes    int64               `json:"objects_bytes"`
	WorkspacesBytes int64               `json:"workspaces_bytes"`
	TotalBytes      int64               `json:"total_bytes"`
	Branches        []BranchStorageRow  `json:"branches"`
}

// BranchStorageRow holds per-branch storage info.
type BranchStorageRow struct {
	Name          string `json:"name"`
	SnapshotCount int    `json:"snapshot_count"`
	TotalBytes    int64  `json:"total_bytes"`
}

// GetStorage returns disk-usage information for the project.
func GetStorage(projectRoot string) (*StorageResult, error) {
	avcDir := filepath.Join(projectRoot, ".avc")

	dbSize := fileSize(filepath.Join(avcDir, "avc.db"))
	objSize, _ := dirSize(filepath.Join(avcDir, "objects"))
	wsSize, _ := dirSize(filepath.Join(avcDir, "workspaces"))

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}

	branches, _ := store.ListBranches(proj.ID)
	branchRows := make([]BranchStorageRow, 0, len(branches))
	for _, b := range branches {
		snaps, _ := store.ListSnapshotsByBranch(b.ID)
		var total int64
		for _, s := range snaps {
			total += s.TotalSize
		}
		branchRows = append(branchRows, BranchStorageRow{
			Name:          b.Name,
			SnapshotCount: len(snaps),
			TotalBytes:    total,
		})
	}
	sort.Slice(branchRows, func(i, j int) bool {
		return branchRows[i].TotalBytes > branchRows[j].TotalBytes
	})

	return &StorageResult{
		ProjectName:     proj.Name,
		DatabaseBytes:   dbSize,
		ObjectsBytes:    objSize,
		WorkspacesBytes: wsSize,
		TotalBytes:      dbSize + objSize + wsSize,
		Branches:        branchRows,
	}, nil
}

// ─── filesystem helpers ───────────────────────────────────────────────────────

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
