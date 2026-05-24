// Package tests — Phase 4: Merge Quality tests.
//
// Covers:
//   4.1 Post-merge auto-snapshot (PostMergeSnapshotID in result; new HEAD on main after clean merge)
//   4.2 Conflict resolution (ListConflicts, ResolveFile: ours/theirs/content)
//   4.3 diff --stat data layer (result carries correct per-file and total line counts)
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	diffpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/merge"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

// ─── 4.1 Post-merge auto-snapshot ────────────────────────────────────────────

// TestMerge_CleanMerge_HasPostMergeSnapshot verifies that a clean merge creates
// a post-merge snapshot on main, and that snapshot becomes the new HEAD.
func TestMerge_CleanMerge_HasPostMergeSnapshot(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	// Snapshot main with one file.
	writeFile(t, root, "readme.md", "# Hello\n")
	createMainSnap(t, root, mainBranchID, "initial")

	// Create a branch and modify the file.
	b, ws := createBranch(t, root, "feat/post-snap")
	writeFile(t, ws, "readme.md", "# Hello\n\nAdded line.\n")
	createBranchSnap(t, root, b.ID, ws, "after edit")

	// Switch back to main for merge.
	if err := branch.Switch(root, "main"); err != nil {
		t.Fatal(err)
	}

	result, err := merge.Merge(root, b.Name)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	if result.PostMergeSnapshotID == "" {
		t.Fatal("expected PostMergeSnapshotID to be set after a clean merge")
	}
	if result.Conflicts != 0 {
		t.Fatalf("expected 0 conflicts, got %d", result.Conflicts)
	}

	// Verify the post-merge snapshot is now HEAD on main.
	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	proj, err := store.GetProject(root)
	if err != nil {
		t.Fatal(err)
	}
	mainBranch, err := store.EnsureMainBranch(proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.GetHeadSnapshot(mainBranch.ID)
	if err != nil {
		t.Fatal(err)
	}

	if head.ID != result.PostMergeSnapshotID {
		t.Errorf("HEAD on main = %q, want post-merge snapshot %q", head.ID, result.PostMergeSnapshotID)
	}
	if !strings.HasPrefix(head.Label, "post-merge:") {
		t.Errorf("post-merge snapshot label = %q, want prefix 'post-merge:'", head.Label)
	}
}

// TestMerge_Conflict_NoPostMergeSnapshot verifies that a merge with conflicts
// does NOT set PostMergeSnapshotID.
func TestMerge_Conflict_NoPostMergeSnapshot(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	writeFile(t, root, "shared.txt", "original\n")
	createMainSnap(t, root, mainBranchID, "base")
	time.Sleep(time.Second)

	b, ws := createBranch(t, root, "feat/no-post-snap")
	writeFile(t, ws, "shared.txt", "branch edit\n")
	createBranchSnap(t, root, b.ID, ws, "branch edit")

	// Main side edit — creates conflict.
	if err := branch.Switch(root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "shared.txt", "main edit\n")
	time.Sleep(time.Second)
	createMainSnap(t, root, mainBranchID, "main edit")

	result, err := merge.Merge(root, b.Name)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	if result.Conflicts == 0 {
		t.Fatal("expected conflicts")
	}
	if result.PostMergeSnapshotID != "" {
		t.Errorf("expected PostMergeSnapshotID empty on conflicted merge, got %q", result.PostMergeSnapshotID)
	}
}

// ─── 4.2 Conflict resolution ──────────────────────────────────────────────────

// setupConflict returns a project root and branch name where a merge conflict
// exists on "target.txt". mainContent and branchContent are the competing versions.
func setupConflict(t *testing.T) (root, branchName, mainContent, branchContent string) {
	t.Helper()
	root, mainBranchID := setupProjectWithMain(t)
	branchName = "feat/resolve-test"
	mainContent = "main version\n"
	branchContent = "branch version\n"

	writeFile(t, root, "target.txt", "base content\n")
	createMainSnap(t, root, mainBranchID, "base")
	time.Sleep(time.Second)

	b, ws := createBranch(t, root, branchName)
	writeFile(t, ws, "target.txt", branchContent)
	createBranchSnap(t, root, b.ID, ws, "branch edit")

	// Main edit — creates conflict.
	if err := branch.Switch(root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "target.txt", mainContent)
	time.Sleep(time.Second)
	createMainSnap(t, root, mainBranchID, "main edit")

	result, err := merge.Merge(root, branchName)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts == 0 {
		t.Fatal("expected conflicts in setup")
	}
	return
}

func TestListConflicts_DetectsMarkedFiles(t *testing.T) {
	root, _, _, _ := setupConflict(t)

	conflicts, err := merge.ListConflicts(root)
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected at least one conflict")
	}
	found := false
	for _, c := range conflicts {
		if c.Path == "target.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected target.txt in conflicts, got %v", conflicts)
	}
}

func TestListConflicts_EmptyAfterResolution(t *testing.T) {
	root, branchName, _, _ := setupConflict(t)

	if err := merge.ResolveFile(root, branchName, "target.txt", "ours", ""); err != nil {
		t.Fatalf("ResolveFile: %v", err)
	}

	conflicts, err := merge.ListConflicts(root)
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts after resolution, got %v", conflicts)
	}
}

func TestResolveFile_Ours(t *testing.T) {
	root, branchName, mainContent, _ := setupConflict(t)

	if err := merge.ResolveFile(root, branchName, "target.txt", "ours", ""); err != nil {
		t.Fatalf("ResolveFile ours: %v", err)
	}

	got := mustReadFile(t, filepath.Join(root, "target.txt"))
	if got != mainContent {
		t.Errorf("after 'ours': got %q, want %q", got, mainContent)
	}
}

func TestResolveFile_Theirs(t *testing.T) {
	root, branchName, _, branchContent := setupConflict(t)

	if err := merge.ResolveFile(root, branchName, "target.txt", "theirs", ""); err != nil {
		t.Fatalf("ResolveFile theirs: %v", err)
	}

	got := mustReadFile(t, filepath.Join(root, "target.txt"))
	if got != branchContent {
		t.Errorf("after 'theirs': got %q, want %q", got, branchContent)
	}
}

func TestResolveFile_Content(t *testing.T) {
	root, branchName, _, _ := setupConflict(t)
	custom := "manually resolved content\n"

	if err := merge.ResolveFile(root, branchName, "target.txt", "content", custom); err != nil {
		t.Fatalf("ResolveFile content: %v", err)
	}

	got := mustReadFile(t, filepath.Join(root, "target.txt"))
	if got != custom {
		t.Errorf("after 'content': got %q, want %q", got, custom)
	}
}

func TestResolveFile_InvalidResolution(t *testing.T) {
	root, branchName, _, _ := setupConflict(t)

	err := merge.ResolveFile(root, branchName, "target.txt", "banana", "")
	if err == nil {
		t.Fatal("expected error for invalid resolution")
	}
}

func TestResolveFile_ContentEmptyReturnsError(t *testing.T) {
	root, branchName, _, _ := setupConflict(t)

	err := merge.ResolveFile(root, branchName, "target.txt", "content", "")
	if err == nil {
		t.Fatal("expected error when content is empty with resolution='content'")
	}
}

// ─── 4.3 diff --stat data layer ──────────────────────────────────────────────

// TestDiffStat_TotalsAreCorrect verifies the diff result carries correct line
// counts — the same data that `avc diff --stat` renders.
func TestDiffStat_TotalsAreCorrect(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	writeFile(t, root, "a.txt", "line1\nline2\n")
	writeFile(t, root, "b.txt", "alpha\nbeta\ngamma\n")
	baseSnap := createMainSnap(t, root, mainBranchID, "base")

	// a.txt: add one line. b.txt: remove one line.
	writeFile(t, root, "a.txt", "line1\nline2\nline3\n")
	writeFile(t, root, "b.txt", "alpha\nbeta\n")
	headSnap := createMainSnap(t, root, mainBranchID, "after edits")

	result, err := diffpkg.Compare(root, baseSnap.ID, headSnap.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("expected 2 changed files, got %d", len(result.Files))
	}

	totalAdded, totalRemoved := 0, 0
	for _, f := range result.Files {
		totalAdded += f.LinesAdded
		totalRemoved += f.LinesRemoved
	}
	if totalAdded == 0 {
		t.Error("expected some added lines")
	}
	if totalRemoved == 0 {
		t.Error("expected some removed lines")
	}
}

// ─── local helpers ────────────────────────────────────────────────────────────

// createBranch creates a named branch and returns the branch record and
// its materialized workspace path.
func createBranch(t *testing.T, root, name string) (*db.Branch, string) {
	t.Helper()
	b, err := branch.Create(root, name, "")
	if err != nil {
		t.Fatalf("branch.Create(%q): %v", name, err)
	}
	if err := branch.Switch(root, name); err != nil {
		t.Fatalf("branch.Switch(%q): %v", name, err)
	}
	ws := branch.WorkspacePath(root, name)
	return b, ws
}

// createBranchSnap creates a snapshot on an agent branch, walking its workspace.
func createBranchSnap(t *testing.T, root, branchID, workspacePath, label string) *snapshot.Result {
	t.Helper()
	snap, err := snapshot.Create(root, label, "test", label, branchID, workspacePath)
	if err != nil {
		t.Fatalf("snapshot.Create(%q on branch %q): %v", label, branchID, err)
	}
	return snap
}

// mustReadFile reads a file and returns its content as a string.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile(%q): %v", path, err)
	}
	return string(data)
}
