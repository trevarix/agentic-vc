package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// setupMergeBase creates a fully initialized project with:
//   - main branch record in the DB
//   - one file on disk with an associated main snapshot
//   - a feature branch created from that snapshot
//   - the branch workspace populated with an edited version of the file
//
// Returns (projectRoot, branchName) ready to test merge operations.
func setupMergeBase(t *testing.T, branchName, originalContent, branchContent string) (string, string) {
	t.Helper()
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "app.go", originalContent)
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "main-base")

	b, err := branch.Create(projectRoot, branchName, baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "app.go", branchContent)
	_, err = snapshot.Create(projectRoot, "branch-edit", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	return projectRoot, branchName
}

// TestMerge_Preview_DoesNotWriteFiles verifies that Preview does not modify
// the project root — it only computes the plan.
func TestMerge_Preview_DoesNotWriteFiles(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/preview", "v1\n", "v2\n")

	originalData, _ := os.ReadFile(filepath.Join(projectRoot, "app.go"))

	result, err := merge.Preview(projectRoot, branchName)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	// File on disk must be untouched.
	afterData, _ := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if string(originalData) != string(afterData) {
		t.Error("Preview must not write to project root")
	}

	// Preview returns no merge ID.
	if result.MergeID != "" {
		t.Errorf("Preview.MergeID should be empty, got %q", result.MergeID)
	}

	if result.BranchName != branchName {
		t.Errorf("BranchName = %q, want %q", result.BranchName, branchName)
	}
}

// TestMerge_CleanMerge_BranchOnlyChanges verifies that when only the branch
// modified a file, the merge is clean with no conflicts.
func TestMerge_CleanMerge_BranchOnlyChanges(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/clean", "original\n", "updated by branch\n")

	result, err := merge.Merge(projectRoot, branchName)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	if result.Conflicts != 0 {
		t.Errorf("expected 0 conflicts, got %d", result.Conflicts)
	}
	if result.Clean < 1 {
		t.Errorf("expected at least 1 clean file, got %d", result.Clean)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	if string(data) != "updated by branch\n" {
		t.Errorf("merged content = %q, want %q", string(data), "updated by branch\n")
	}
}

// TestMerge_CleanMerge_BranchAddsNewFile verifies that a new file added only
// by the branch merges cleanly into main.
func TestMerge_CleanMerge_BranchAddsNewFile(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "existing.go", "package main\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "main-base")

	b, err := branch.Create(projectRoot, "feat/new-file", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	// Copy existing file to workspace (branch sees same existing.go).
	writeFile(t, ws, "existing.go", "package main\n")
	// Add new file to workspace.
	writeFile(t, ws, "new_feature.go", "package main\n\nfunc NewFeature() {}\n")

	_, err = snapshot.Create(projectRoot, "add new file", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	result, err := merge.Merge(projectRoot, "feat/new-file")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	if result.Conflicts != 0 {
		t.Errorf("expected 0 conflicts, got %d", result.Conflicts)
	}

	newFilePath := filepath.Join(projectRoot, "new_feature.go")
	if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
		t.Error("new_feature.go should exist in project root after merge")
	}
}

// TestMerge_Conflict_BothSidesModifiedDifferently verifies that when both main
// and branch changed the same file to different content, the merge produces
// conflict markers.
func TestMerge_Conflict_BothSidesModifiedDifferently(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	// Use distinct sizes to defeat any stat-cache false-hit (mtime+size cache).
	writeFile(t, projectRoot, "shared.go", "base\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Branch edits shared.go.
	b, err := branch.Create(projectRoot, "feat/conflict", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "shared.go", "branch-only content that is much longer\n")
	_, err = snapshot.Create(projectRoot, "branch edit", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Main ALSO edits shared.go (different content and different size).
	// Sleep ensures the new snapshot gets a strictly later Unix timestamp than
	// the base snapshot so GetHeadSnapshot reliably returns the right snapshot.
	time.Sleep(time.Second)
	writeFile(t, projectRoot, "shared.go", "main-only content, also longer than base\n")
	createMainSnap(t, projectRoot, mainBranchID, "main edit")

	result, err := merge.Merge(projectRoot, "feat/conflict")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	if result.Conflicts < 1 {
		t.Errorf("expected at least 1 conflict, got %d", result.Conflicts)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "shared.go"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "<<<<<<< main (ours)") {
		t.Error("conflict marker '<<<<<<< main (ours)' missing from file")
	}
	if !strings.Contains(content, ">>>>>>> branch (theirs)") {
		t.Error("conflict marker '>>>>>>> branch (theirs)' missing from file")
	}
	if !strings.Contains(content, "=======") {
		t.Error("conflict separator '=======' missing from file")
	}
}

// TestMerge_Skip_FilesUnchangedInBranch verifies that files not modified by
// the branch are marked as skipped in the preview.
func TestMerge_Skip_FilesUnchangedInBranch(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "unchanged.go", "stable content\n")
	writeFile(t, projectRoot, "changed.go", "original\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/partial", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	// Branch only modifies changed.go; unchanged.go stays identical to base.
	writeFile(t, ws, "changed.go", "branch updated\n")
	writeFile(t, ws, "unchanged.go", "stable content\n")

	_, err = snapshot.Create(projectRoot, "branch changes", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	result, err := merge.Preview(projectRoot, "feat/partial")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	var skippedUnchanged int
	for _, f := range result.Files {
		if f.Path == "unchanged.go" && f.Decision == "skip" {
			skippedUnchanged++
		}
	}
	if skippedUnchanged == 0 {
		t.Error("expected unchanged.go to have decision=skip in preview")
	}
}

// TestMerge_Abort_AfterConflict verifies that aborting a conflicted merge
// restores main to its exact pre-merge state and clears the conflict
// markers. merge.Abort locates the last merge by project ID — a lookup by
// main's branch ID would never match, since merges are recorded under the
// *agent* branch's ID (see docs/plans/02-merge-integrity.md).
func TestMerge_Abort_AfterConflict(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "base\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/abort", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "file.go", "branch-version — longer\n")
	_, err = snapshot.Create(projectRoot, "branch", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Advance main so the merge produces a conflict.
	time.Sleep(time.Second)
	const mainContent = "main-version — also longer\n"
	writeFile(t, projectRoot, "file.go", mainContent)
	createMainSnap(t, projectRoot, mainBranchID, "main advance")

	mergeResult, err := merge.Merge(projectRoot, "feat/abort")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if mergeResult.Conflicts == 0 {
		t.Fatal("expected a conflict from two divergent edits to the same file, got none")
	}

	if err := merge.Abort(projectRoot); err != nil {
		t.Fatalf("abort: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "file.go"))
	if err != nil {
		t.Fatalf("read file after abort: %v", err)
	}
	if strings.Contains(string(data), "<<<<<<<") {
		t.Error("conflict markers still present after abort")
	}
	if string(data) != mainContent {
		t.Errorf("main content after abort = %q, want %q (pre-merge state)", data, mainContent)
	}
}

// TestMerge_Abort_AfterFailure verifies that a merge which fails partway
// through applying its plan marks the merge record "failed" (not stuck at
// "in_progress" forever), and that abort can find and roll back that failed
// attempt just like it does for conflicts.
func TestMerge_Abort_AfterFailure(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "file.go", "original\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/inject-failure", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "newfile.txt", "branch added this\n")
	if _, err := snapshot.Create(projectRoot, "branch-adds-file", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Sabotage the merge: pre-create a directory at the exact path the
	// branch's new file would be written to, so applyPlan's write fails.
	if err := os.MkdirAll(filepath.Join(projectRoot, "newfile.txt"), 0755); err != nil {
		t.Fatalf("sabotage setup: %v", err)
	}

	if _, err := merge.Merge(projectRoot, "feat/inject-failure"); err == nil {
		t.Fatal("expected merge to fail because the destination path is a directory")
	}

	if err := merge.Abort(projectRoot); err != nil {
		t.Fatalf("abort after failed merge: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "file.go"))
	if err != nil {
		t.Fatalf("read file.go after abort: %v", err)
	}
	if string(data) != "original\n" {
		t.Errorf("file.go after abort = %q, want %q (pre-merge state)", data, "original\n")
	}
}

// TestMerge_Abort_FailsWhenNoMergeInProgress verifies that aborting when there
// is no active merge returns an error.
func TestMerge_Abort_FailsWhenNoMergeInProgress(t *testing.T) {
	projectRoot := setupTestProject(t)

	if err := merge.Abort(projectRoot); err == nil {
		t.Error("expected error when no merge is in progress, got nil")
	}
}

// TestMerge_ErrorOnUnknownBranch verifies that merging a non-existent branch
// returns an error.
func TestMerge_ErrorOnUnknownBranch(t *testing.T) {
	projectRoot := setupTestProject(t)

	_, err := merge.Merge(projectRoot, "no-such-branch")
	if err == nil {
		t.Error("expected error for unknown branch, got nil")
	}
}

// TestMerge_ErrorWhenBranchHasNoSnapshots verifies that attempting to merge a
// branch that exists but has no snapshots returns an error.
// TestMerge_AutoSnapshotsUnsnapshottedBranchBeforeMerging verifies that
// merging a branch which was created but never explicitly snapshotted still
// succeeds: Merge's dirty-workspace guard (Plan 02 item 3) captures the
// workspace's current state first rather than silently dropping it or
// erroring out. Before that guard existed, this used to be an error.
func TestMerge_AutoSnapshotsUnsnapshottedBranchBeforeMerging(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Create a branch but do NOT take a snapshot on it.
	b, err := branch.Create(projectRoot, "empty-branch", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	result, err := merge.Merge(projectRoot, b.Name)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.AutoSnapshotID == "" {
		t.Error("expected an auto-snapshot to be recorded for the previously un-snapshotted branch")
	}
}

// TestMerge_AutoSnapshotsMainBeforeWriting verifies that Merge automatically
// creates a pre-merge snapshot on main before writing files.
func TestMerge_AutoSnapshotsMainBeforeWriting(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/autosnap", "v1\n", "v2\n")

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	snapsBefore, _ := store.ListSnapshots()
	store.Close()

	_, err = merge.Merge(projectRoot, branchName)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store2.Close()
	snapsAfter, _ := store2.ListSnapshots()

	if len(snapsAfter) <= len(snapsBefore) {
		t.Errorf("expected more snapshots after merge (auto-snapshot), before=%d after=%d",
			len(snapsBefore), len(snapsAfter))
	}
}

// TestMerge_Preview_ReturnsCorrectCounts verifies that Clean + Conflicts +
// Skipped equals the total number of files in the result.
func TestMerge_Preview_ReturnsCorrectCounts(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/counts", "original\n", "edited\n")

	result, err := merge.Preview(projectRoot, branchName)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	total := result.Clean + result.Conflicts + result.Skipped
	if total != len(result.Files) {
		t.Errorf("counts don't add up: clean=%d conflicts=%d skipped=%d total=%d files=%d",
			result.Clean, result.Conflicts, result.Skipped, total, len(result.Files))
	}
}
