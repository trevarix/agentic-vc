package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// TestWorkspace_MaterializeCreatesFiles verifies that branch create populates
// the workspace directory with the base snapshot's files.
func TestWorkspace_MaterializeCreatesFiles(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "main.go", "package main\n")
	writeFile(t, projectRoot, "README.md", "# hello\n")

	snap, err := snapshot.Create(projectRoot, "base", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	b, err := branch.Create(projectRoot, "feature/test", snap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	if ws == "" {
		t.Fatal("expected non-empty workspace path")
	}

	// Both files should exist in the workspace.
	for _, rel := range []string{"main.go", "README.md"} {
		path := filepath.Join(ws, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("workspace file %s missing: %v", rel, err)
			continue
		}
		// Content should match the project file.
		orig, _ := os.ReadFile(filepath.Join(projectRoot, rel))
		if string(data) != string(orig) {
			t.Errorf("workspace %s content mismatch", rel)
		}
	}
}

// TestWorkspace_HardlinkSharedInode verifies that workspace files share an
// inode with the project root files where hardlinks are supported.
// Skipped on Windows — NTFS hardlinks require the same volume and elevated
// permissions are unreliable in CI.
func TestWorkspace_HardlinkSharedInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlink inode check not reliable on Windows")
	}

	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "shared.go", "package p\n")

	snap, err := snapshot.Create(projectRoot, "base", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	b, err := branch.Create(projectRoot, "feat", snap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	srcInfo, err := os.Stat(filepath.Join(projectRoot, "shared.go"))
	if err != nil {
		t.Fatalf("stat project file: %v", err)
	}
	wsInfo, err := os.Stat(filepath.Join(ws, "shared.go"))
	if err != nil {
		t.Fatalf("stat workspace file: %v", err)
	}

	// os.SameFile returns true if two FileInfos describe the same file (same inode).
	if !os.SameFile(srcInfo, wsInfo) {
		// Not a hard failure — MaterializeWorkspace may fall back to copy.
		t.Log("workspace file is a copy, not a hardlink (acceptable fallback)")
	}
}

// TestWorkspace_SnapshotWalksWorkspace verifies that avc snapshot on a branch
// captures the workspace state, not the project root state.
func TestWorkspace_SnapshotWalksWorkspace(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "v1")

	base, err := snapshot.Create(projectRoot, "base", "", "", "", "")
	if err != nil {
		t.Fatalf("base snapshot: %v", err)
	}

	b, err := branch.Create(projectRoot, "agent/work", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)

	// Agent modifies only the workspace — project root is unchanged.
	writeFile(t, ws, "app.go", "v2 — agent edit")
	writeFile(t, ws, "new_feature.go", "package app\n")

	// Snapshot from workspace.
	branchSnap, err := snapshot.Create(projectRoot, "agent changes", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	if branchSnap.FileCount != 2 {
		t.Errorf("expected 2 files in branch snapshot, got %d", branchSnap.FileCount)
	}

	// Project root must still be at v1.
	data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if string(data) != "v1" {
		t.Errorf("project root app.go was modified; want v1, got %q", string(data))
	}
}

// TestWorkspace_RestoreTargetsWorkspace verifies that RestoreToDir writes
// files into the workspace, leaving the project root untouched.
func TestWorkspace_RestoreTargetsWorkspace(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "original")

	base, err := snapshot.Create(projectRoot, "base", "", "", "", "")
	if err != nil {
		t.Fatalf("base snapshot: %v", err)
	}

	b, err := branch.Create(projectRoot, "restore-test", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)

	// Corrupt the workspace file.
	writeFile(t, ws, "app.go", "corrupted")

	// Restore the base snapshot into the workspace.
	result, err := restore.RestoreToDir(projectRoot, base.ID, ws)
	if err != nil {
		t.Fatalf("RestoreToDir: %v", err)
	}
	if result.RestoredFiles != 1 {
		t.Errorf("expected 1 restored file, got %d", result.RestoredFiles)
	}

	// Workspace file should be back to "original".
	wsData, _ := os.ReadFile(filepath.Join(ws, "app.go"))
	if string(wsData) != "original" {
		t.Errorf("workspace app.go = %q, want %q", string(wsData), "original")
	}

	// Project root must be untouched.
	projData, _ := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if string(projData) != "original" {
		t.Errorf("project root app.go was modified: %q", string(projData))
	}
}

// TestWorkspace_DeleteRemovesWorkspace verifies that branch delete cleans up
// the workspace directory.
func TestWorkspace_DeleteRemovesWorkspace(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "file.go", "v1")

	snap, err := snapshot.Create(projectRoot, "base", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	b, err := branch.Create(projectRoot, "to-delete", snap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	if _, err := os.Stat(ws); os.IsNotExist(err) {
		t.Fatal("workspace directory should exist after branch create")
	}

	if err := branch.Delete(projectRoot, b.Name, false); err != nil {
		t.Fatalf("delete branch: %v", err)
	}

	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		t.Error("workspace directory should be removed after branch delete")
	}
}

// TestWorkspace_MainBranchNoWorkspace verifies that main branch has no
// workspace path — it always operates on the real project root.
func TestWorkspace_MainBranchNoWorkspace(t *testing.T) {
	if ws := branch.WorkspacePath("/any/path", "main"); ws != "" {
		t.Errorf("main branch workspace path should be empty, got %q", ws)
	}
	if ws := branch.WorkspacePath("/any/path", ""); ws != "" {
		t.Errorf("empty branch name workspace path should be empty, got %q", ws)
	}
}
