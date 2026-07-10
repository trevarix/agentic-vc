// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests for docs/plans/02-merge-integrity.md.
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// ─── 1 · Deletion-aware merge + no panic (review 1.2) ─────────────────────────

// TestMerge_BranchDeletesFile_NoConflict reproduces the review's crash
// scenario: a branch deletes a file that base and main left unchanged. The
// merge must complete (no panic) and remove the file from main.
func TestMerge_BranchDeletesFile_NoConflict(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "keep.txt", "keep me")
	writeFile(t, projectRoot, "remove.txt", "delete me")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/delete-file", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	if err := os.Remove(filepath.Join(ws, "remove.txt")); err != nil {
		t.Fatalf("remove workspace file: %v", err)
	}
	if _, err := snapshot.Create(projectRoot, "deleted remove.txt", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	result, err := merge.Merge(projectRoot, "feat/delete-file")
	if err != nil {
		t.Fatalf("merge: %v (should not panic or error)", err)
	}
	if result.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", result.Deleted)
	}
	if result.Conflicts != 0 {
		t.Errorf("Conflicts = %d, want 0", result.Conflicts)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, "remove.txt")); !os.IsNotExist(err) {
		t.Errorf("remove.txt should be gone from main, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "keep.txt")); err != nil {
		t.Errorf("keep.txt should still exist: %v", err)
	}
}

// TestMerge_DeleteVsEdit_ConflictHasClearLabels verifies that a file deleted
// on the branch but edited on main produces a clearly labeled conflict
// instead of a panic or a silent decision either way.
func TestMerge_DeleteVsEdit_ConflictHasClearLabels(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "contested.txt", "base content\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/delete-vs-edit", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	if err := os.Remove(filepath.Join(ws, "contested.txt")); err != nil {
		t.Fatalf("remove workspace file: %v", err)
	}
	if _, err := snapshot.Create(projectRoot, "branch deletes it", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Main edits the same file after the branch was created.
	writeFile(t, projectRoot, "contested.txt", "main edited it\n")
	createMainSnap(t, projectRoot, mainBranchID, "main edits")

	result, err := merge.Merge(projectRoot, "feat/delete-vs-edit")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1", result.Conflicts)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "contested.txt"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "file deleted on branch") {
		t.Errorf("conflict markers should label the deleted side clearly, got:\n%s", content)
	}
	if !strings.Contains(content, "main edited it") {
		t.Errorf("conflict content should include main's edit, got:\n%s", content)
	}
}

// ─── 2 · Merge failure never leaves in_progress; abort works (review 1.3) ─────

// TestGetLastMergeForProject_ReturnsMostRecentAcrossBranches verifies the
// project-scoped merge lookup used by Abort: it must find the latest merge
// regardless of which agent branch it was recorded under (a lookup keyed on
// main's branch ID would never match, since merges are recorded under the
// agent branch's ID).
func TestGetLastMergeForProject_ReturnsMostRecentAcrossBranches(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "a.txt", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b1, err := branch.Create(projectRoot, "feat/one", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch one: %v", err)
	}
	if _, err := merge.Merge(projectRoot, b1.Name); err != nil {
		t.Fatalf("merge one: %v", err)
	}

	writeFile(t, projectRoot, "b.txt", "v1")
	baseSnap2 := createMainSnap(t, projectRoot, mainBranchID, "base2")
	b2, err := branch.Create(projectRoot, "feat/two", baseSnap2.ID)
	if err != nil {
		t.Fatalf("create branch two: %v", err)
	}
	mergeResult2, err := merge.Merge(projectRoot, b2.Name)
	if err != nil {
		t.Fatalf("merge two: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	last, err := store.GetLastMergeForProject(proj.ID)
	if err != nil {
		t.Fatalf("GetLastMergeForProject: %v", err)
	}
	if last.ID != mergeResult2.MergeID {
		t.Errorf("GetLastMergeForProject returned %q, want the most recent merge %q", last.ID, mergeResult2.MergeID)
	}
}

// ─── 3 · Dirty-workspace guard on merge (review 2.7) ──────────────────────────

// TestMerge_CapturesUnsnapshottedWorkspaceChanges verifies that edits made in
// the workspace after the branch's last snapshot are captured automatically
// before the merge, instead of being silently dropped and then destroyed
// when the workspace is removed on a successful merge.
func TestMerge_CapturesUnsnapshottedWorkspaceChanges(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/dirty-workspace", "v1\n", "v2\n")

	// Edit the workspace again WITHOUT taking a new snapshot.
	ws := branch.WorkspacePath(projectRoot, branchName)
	writeFile(t, ws, "app.go", "v3-unsnapshotted\n")

	result, err := merge.Merge(projectRoot, branchName)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.AutoSnapshotID == "" {
		t.Error("expected AutoSnapshotID to be set for the dirty workspace")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	if string(data) != "v3-unsnapshotted\n" {
		t.Errorf("merged content = %q, want the un-snapshotted workspace edit %q", data, "v3-unsnapshotted\n")
	}
}

// TestMergePreview_ReportsDirtyWorkspaceWithoutSnapshotting verifies that
// Preview surfaces un-snapshotted workspace changes as a count, but — unlike
// Merge — never creates a snapshot as a side effect (Preview's documented
// contract is "no writes, no recorded merge").
func TestMergePreview_ReportsDirtyWorkspaceWithoutSnapshotting(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/preview-dirty", "v1\n", "v2\n")

	ws := branch.WorkspacePath(projectRoot, branchName)
	writeFile(t, ws, "app.go", "v3-unsnapshotted\n")

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	b, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	before, err := store.ListSnapshotsByBranch(b.ID)
	store.Close()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}

	result, err := merge.Preview(projectRoot, branchName)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.WorkspaceDirtyFiles == 0 {
		t.Error("expected WorkspaceDirtyFiles > 0 for the un-snapshotted edit")
	}

	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	after, err := store2.ListSnapshotsByBranch(b.ID)
	store2.Close()
	if err != nil {
		t.Fatalf("list snapshots after preview: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("Preview must not create snapshots as a side effect: had %d, now %d", len(before), len(after))
	}
}

// ─── 4 · Pre-restore safety snapshot (review 2.8) ─────────────────────────────

// TestCreateBeforeRestore_CapturesDirtyTree verifies the shared safety-net
// helper used by every restore surface (CLI, MCP, web): a working tree that
// has changed since its last snapshot is captured before a restore
// overwrites it.
func TestCreateBeforeRestore_CapturesDirtyTree(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "v1")
	createMainSnap(t, projectRoot, mainBranchID, "v1")

	// Dirty the tree without snapshotting.
	writeFile(t, projectRoot, "app.go", "v2-uncommitted")

	preSnap, err := snapshot.CreateBeforeRestore(projectRoot, projectRoot, mainBranchID, "irrelevant-target-id")
	if err != nil {
		t.Fatalf("CreateBeforeRestore: %v", err)
	}
	if preSnap == nil {
		t.Fatal("expected a pre-restore snapshot for a dirty tree, got nil")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	files, err := store.GetSnapshotFiles(preSnap.ID)
	if err != nil {
		t.Fatalf("get snapshot files: %v", err)
	}
	found := false
	for _, f := range files {
		if f.RelativePath == "app.go" {
			found = true
		}
	}
	if !found {
		t.Error("pre-restore snapshot should include app.go")
	}
}

// TestCreateBeforeRestore_SkipsCleanTree verifies that a working tree which
// already matches its branch's HEAD snapshot is not re-snapshotted.
func TestCreateBeforeRestore_SkipsCleanTree(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "v1")
	createMainSnap(t, projectRoot, mainBranchID, "v1")

	preSnap, err := snapshot.CreateBeforeRestore(projectRoot, projectRoot, mainBranchID, "irrelevant-target-id")
	if err != nil {
		t.Fatalf("CreateBeforeRestore: %v", err)
	}
	if preSnap != nil {
		t.Errorf("expected no snapshot for a clean tree, got %q", preSnap.ID)
	}
}
