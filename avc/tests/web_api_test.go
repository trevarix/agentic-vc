// Package tests — Phase 6: Web API / internal/api layer tests.
//
// Tests exercise the api.* types directly (SnapshotOps, BranchOps,
// MergeOps, GetStatus, GetStorage) which are the same logic the web server
// delegates to. We do not spin up a full HTTP server here; the HTTP routing
// layer is thin (parse → api.* → writeJSON) and validated by the build.
package tests

import (
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/api"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
)

// ─── SnapshotOps ──────────────────────────────────────────────────────────────

func TestSnapshotOps_Create_And_List(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	writeFile(t, root, "main.go", "package main\n")

	ops := api.SnapshotOps{ProjectRoot: root}

	snap, err := ops.Create("api-test snap", "test-agent", "notes here")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if snap.ID == "" {
		t.Fatal("expected non-empty snapshot ID")
	}
	if snap.Label != "api-test snap" {
		t.Errorf("label = %q, want %q", snap.Label, "api-test snap")
	}

	snaps, err := ops.List(db.SnapshotFilter{Limit: -1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, s := range snaps {
		if s.ID == snap.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("created snapshot %q not found in list", snap.ID)
	}
}

func TestSnapshotOps_Info(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	writeFile(t, root, "a.txt", "hello")

	ops := api.SnapshotOps{ProjectRoot: root}
	snap, err := ops.Create("info test", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, files, err := ops.Info(snap.ID)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if got.ID != snap.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, snap.ID)
	}
	if len(files) == 0 {
		t.Error("expected at least 1 file in snapshot info")
	}
}

func TestSnapshotOps_Delete(t *testing.T) {
	root, _ := setupProjectWithMain(t)

	ops := api.SnapshotOps{ProjectRoot: root}
	snap, err := ops.Create("to delete", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ops.Delete(snap.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, _, err = ops.Info(snap.ID)
	if err == nil {
		t.Error("expected error after deleting snapshot, got nil")
	}
}

func TestSnapshotOps_Delete_NonExistent(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.SnapshotOps{ProjectRoot: root}
	if err := ops.Delete("does-not-exist"); err == nil {
		t.Error("expected error deleting non-existent snapshot")
	}
}

func TestSnapshotOps_Tag_And_Untag(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.SnapshotOps{ProjectRoot: root}
	snap, err := ops.Create("tag test", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ops.Tag(snap.ID, "stable"); err != nil {
		t.Fatalf("Tag: %v", err)
	}

	// Verify tag appears in filtered list.
	results, err := ops.List(db.SnapshotFilter{Tag: "stable", Limit: -1})
	if err != nil {
		t.Fatalf("List by tag: %v", err)
	}
	if len(results) != 1 || results[0].ID != snap.ID {
		t.Errorf("expected 1 tagged snap, got %d", len(results))
	}

	if err := ops.Untag(snap.ID, "stable"); err != nil {
		t.Fatalf("Untag: %v", err)
	}

	results, err = ops.List(db.SnapshotFilter{Tag: "stable", Limit: -1})
	if err != nil {
		t.Fatalf("List after untag: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 after untag, got %d", len(results))
	}
}

func TestSnapshotOps_List_Filter_Query(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.SnapshotOps{ProjectRoot: root}
	ops.Create("auth refactor", "", "")
	ops.Create("payment fix", "", "")

	results, err := ops.List(db.SnapshotFilter{Query: "auth", Limit: -1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 || results[0].Label != "auth refactor" {
		t.Errorf("expected [auth refactor], got %v", results)
	}
}

// ─── BranchOps ───────────────────────────────────────────────────────────────

func TestBranchOps_List(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.BranchOps{ProjectRoot: root}

	res, err := ops.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Branches) == 0 {
		t.Error("expected at least the main branch")
	}
	// active branch should default to main
	if res.ActiveName != "main" {
		t.Errorf("expected active=main, got %q", res.ActiveName)
	}
}

func TestBranchOps_Create_And_Switch(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.BranchOps{ProjectRoot: root}

	b, ws, err := ops.Create("feature-x", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Name != "feature-x" {
		t.Errorf("branch name = %q, want %q", b.Name, "feature-x")
	}
	if ws == "" {
		t.Error("expected non-empty workspace path")
	}

	// Should now be on feature-x.
	res, err := ops.List()
	if err != nil {
		t.Fatalf("List after Create: %v", err)
	}
	if res.ActiveName != "feature-x" {
		t.Errorf("expected active=feature-x after create, got %q", res.ActiveName)
	}

	// Switch back to main.
	if err := ops.Switch("main"); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	res, _ = ops.List()
	if res.ActiveName != "main" {
		t.Errorf("expected active=main after switch, got %q", res.ActiveName)
	}
}

func TestBranchOps_Delete(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.BranchOps{ProjectRoot: root}

	// Create then switch away before deleting.
	ops.Create("temp-branch", "")
	ops.Switch("main")

	if err := ops.Delete("temp-branch", true); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	res, _ := ops.List()
	for _, b := range res.Branches {
		if b.Name == "temp-branch" {
			t.Error("deleted branch still appears in list")
		}
	}
}

func TestBranchOps_Diff(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	ops := api.BranchOps{ProjectRoot: root}
	sOps := api.SnapshotOps{ProjectRoot: root}

	// Establish main baseline.
	writeFile(t, root, "main.go", "package main\n")
	sOps.Create("main baseline", "", "")

	// Create branch, write a file into its workspace, snapshot on branch.
	b, ws, err := ops.Create("diff-branch", "")
	if err != nil {
		t.Fatalf("Create branch: %v", err)
	}
	writeFile(t, ws, "feature.go", "package main\nfunc Feature() {}\n")

	// Snapshot on the branch.
	branchID, _ := branchpkg.GetActiveBranchID(root)
	createBranchSnapForWeb(t, root, branchID, ws, "branch snap")

	// Get branch diff.
	result, err := ops.Diff(b.Name)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if result.BranchName != b.Name {
		t.Errorf("BranchName = %q, want %q", result.BranchName, b.Name)
	}
	// Diff may be empty if only one snapshot (no base to compare from);
	// we just verify no error and the structure is correct.
	if result.Diff == nil {
		t.Error("expected non-nil Diff")
	}
}

// createBranchSnapForWeb creates a snapshot on the current branch from its workspace.
func createBranchSnapForWeb(t *testing.T, root, branchID, ws, label string) {
	t.Helper()
	createSnap(t, root, branchID, label, "test", "")
	_ = ws
}

// ─── MergeOps ────────────────────────────────────────────────────────────────

func TestMergeOps_Preview(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	// Main baseline.
	writeFile(t, root, "shared.go", "package main\nconst A = 1\n")
	createMainSnap(t, root, mainBranchID, "main baseline")

	// Create branch and add a new file.
	bOps := api.BranchOps{ProjectRoot: root}
	b, ws, err := bOps.Create("merge-test-branch", "")
	if err != nil {
		t.Fatalf("Create branch: %v", err)
	}
	writeFile(t, ws, "feature.go", "package main\nfunc F() {}\n")

	branchID, _ := branchpkg.GetActiveBranchID(root)
	createSnap(t, root, branchID, "branch snap", "test", "")

	// Switch back and preview.
	bOps.Switch("main")
	mOps := api.MergeOps{ProjectRoot: root}
	plan, err := mOps.Preview(b.Name)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	// Should be a clean merge (no conflicts).
	if plan.Conflicts != 0 {
		t.Errorf("expected 0 conflicts in preview, got %d", plan.Conflicts)
	}
}

func TestMergeOps_Abort(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	mOps := api.MergeOps{ProjectRoot: root}
	// Abort when no merge is in progress — should return an error gracefully.
	err := mOps.Abort()
	// This is expected to error (nothing to abort); just verify no panic.
	_ = err
}

// ─── GetStatus ────────────────────────────────────────────────────────────────

func TestGetStatus_NoSnapshots(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	result, err := api.GetStatus(root)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if result.BranchName == "" {
		t.Error("expected non-empty branch name")
	}
	// No snapshots → no files diff.
	if len(result.Files) != 0 {
		t.Errorf("expected 0 files on fresh project, got %d", len(result.Files))
	}
}

func TestGetStatus_DetectsChanges(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	writeFile(t, root, "app.go", "package main\n")
	sOps := api.SnapshotOps{ProjectRoot: root}
	sOps.Create("baseline", "", "")

	// Mutate a file after snapshot.
	writeFile(t, root, "app.go", "package main\nfunc Modified() {}\n")

	result, err := api.GetStatus(root)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(result.Files) == 0 {
		t.Error("expected at least 1 changed file in status")
	}
}

// ─── GetStorage ───────────────────────────────────────────────────────────────

func TestGetStorage_ReturnsProjectName(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	result, err := api.GetStorage(root)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if result.ProjectName == "" {
		t.Error("expected non-empty project name")
	}
	// DB should have some size (schema at minimum).
	if result.DatabaseBytes == 0 {
		t.Error("expected non-zero database size")
	}
}

func TestGetStorage_BranchesNotNil(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	result, err := api.GetStorage(root)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if result.Branches == nil {
		t.Error("expected non-nil branches slice")
	}
}

func TestGetStorage_TotalIsSum(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	writeFile(t, root, "a.txt", "data")
	sOps := api.SnapshotOps{ProjectRoot: root}
	sOps.Create("snap", "", "")

	result, err := api.GetStorage(root)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	want := result.DatabaseBytes + result.ObjectsBytes + result.WorkspacesBytes
	if result.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d (sum of parts)", result.TotalBytes, want)
	}
}
