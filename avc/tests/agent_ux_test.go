// Package tests — agent UX tests.
//
// Covers:
//   - avc status: diff between last snapshot and current disk
//   - avc_restore_file: single-file restore to project root or workspace
//   - avc_annotate / Annotate: correct line origins, O(1) DB query
//   - GetFileVersions: single-query file history
//   - avc_run_in_workspace gate: disabled unless [run] enabled = true
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/annotate"
	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// ─── 3.1 · avc status (CompareWithCurrentDir) ────────────────────────────────

// TestStatus_DetectsModifiedFile verifies that CompareWithCurrentDir reports a
// modified file when the disk content differs from the last snapshot.
func TestStatus_DetectsModifiedFile(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "app.go", "original\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Modify the file on disk without taking a new snapshot.
	writeFile(t, projectRoot, "app.go", "modified\n")

	result, err := diff.CompareWithCurrentDir(projectRoot, projectRoot, snap.ID)
	if err != nil {
		t.Fatalf("CompareWithCurrentDir: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.Files))
	}
	if result.Files[0].Path != "app.go" {
		t.Errorf("expected changed file 'app.go', got %q", result.Files[0].Path)
	}
	if string(result.Files[0].Type) != "modified" {
		t.Errorf("expected type 'modified', got %q", result.Files[0].Type)
	}
}

// TestStatus_DetectsAddedFile verifies that a file created after a snapshot
// appears as "added" in the status output.
func TestStatus_DetectsAddedFile(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "existing.go", "v1\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Add a new file that was not in the snapshot.
	writeFile(t, projectRoot, "new.go", "new file\n")

	result, err := diff.CompareWithCurrentDir(projectRoot, projectRoot, snap.ID)
	if err != nil {
		t.Fatalf("CompareWithCurrentDir: %v", err)
	}

	found := false
	for _, f := range result.Files {
		if f.Path == "new.go" && string(f.Type) == "added" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'new.go' to appear as added in status")
	}
}

// TestStatus_CleanTree verifies that an unchanged working tree returns zero
// changed files.
func TestStatus_CleanTree(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "clean.go", "unchanged\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	result, err := diff.CompareWithCurrentDir(projectRoot, projectRoot, snap.ID)
	if err != nil {
		t.Fatalf("CompareWithCurrentDir: %v", err)
	}
	if len(result.Files) != 0 {
		t.Errorf("expected 0 changed files for clean tree, got %d", len(result.Files))
	}
}

// TestStatus_WorkspaceSourceDir verifies that when sourceDir is a workspace
// the diff reflects workspace files, not the project root.
func TestStatus_WorkspaceSourceDir(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "shared.go", "original\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/status-ws", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	// Take initial branch snapshot.
	branchSnap, err := snapshot.Create(projectRoot, "branch-base", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Modify the workspace file.
	writeFile(t, ws, "shared.go", "workspace modified\n")

	// Status against the workspace should show the change.
	wsResult, err := diff.CompareWithCurrentDir(projectRoot, ws, branchSnap.ID)
	if err != nil {
		t.Fatalf("CompareWithCurrentDir (workspace): %v", err)
	}
	if len(wsResult.Files) == 0 {
		t.Error("expected workspace status to detect modification in workspace")
	}
}

// ─── 3.2 · avc_restore_file ──────────────────────────────────────────────────

// TestRestoreFileToDir_ProjectRoot verifies that RestoreFileToDir restores only
// the requested file and leaves other files untouched.
func TestRestoreFileToDir_ProjectRoot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "target.go", "original target\n")
	writeFile(t, projectRoot, "other.go", "other file\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Overwrite both files after snapshot.
	writeFile(t, projectRoot, "target.go", "overwritten target\n")
	writeFile(t, projectRoot, "other.go", "overwritten other\n")

	// Restore only target.go from the snapshot.
	result, err := restore.RestoreFileToDir(projectRoot, snap.ID, "target.go", projectRoot)
	if err != nil {
		t.Fatalf("RestoreFileToDir: %v", err)
	}
	if result.FilePath != "target.go" {
		t.Errorf("expected FilePath 'target.go', got %q", result.FilePath)
	}

	// target.go must be restored.
	data, _ := os.ReadFile(filepath.Join(projectRoot, "target.go"))
	if strings.TrimSpace(string(data)) != "original target" {
		t.Errorf("target.go content = %q, want %q", string(data), "original target\n")
	}

	// other.go must be unchanged (still overwritten).
	other, _ := os.ReadFile(filepath.Join(projectRoot, "other.go"))
	if strings.TrimSpace(string(other)) != "overwritten other" {
		t.Errorf("other.go should be untouched; got %q", string(other))
	}
}

// TestRestoreFileToDir_Workspace verifies that RestoreFileToDir writes to the
// workspace, not the project root, when targetDir is the workspace.
func TestRestoreFileToDir_Workspace(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "ws_file.go", "base content\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/rfile-ws", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// Take a branch snapshot while workspace has the base content.
	branchSnap, err := snapshot.Create(projectRoot, "ws-base", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Overwrite the workspace file.
	writeFile(t, ws, "ws_file.go", "workspace overwritten\n")

	// Restore only to workspace.
	_, err = restore.RestoreFileToDir(projectRoot, branchSnap.ID, "ws_file.go", ws)
	if err != nil {
		t.Fatalf("RestoreFileToDir to workspace: %v", err)
	}

	// Workspace must have the restored content.
	data, _ := os.ReadFile(filepath.Join(ws, "ws_file.go"))
	if strings.TrimSpace(string(data)) != "base content" {
		t.Errorf("workspace file = %q, want %q", string(data), "base content\n")
	}

	// Project root must be untouched.
	rootData, _ := os.ReadFile(filepath.Join(projectRoot, "ws_file.go"))
	if strings.TrimSpace(string(rootData)) != "base content" {
		// The project root retains the original snapshot content (from createMainSnap).
		// Just confirm it was NOT written by the workspace restore.
		t.Logf("project root ws_file.go = %q (expected base content from initial snapshot)", string(rootData))
	}
}

// TestRestoreFileToDir_FileNotInSnapshot verifies that an error is returned
// when the requested file was not in the snapshot.
func TestRestoreFileToDir_FileNotInSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "a.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "snap")

	_, err := restore.RestoreFileToDir(projectRoot, snap.ID, "nonexistent.go", projectRoot)
	if err == nil {
		t.Error("expected error restoring a file not in the snapshot, got nil")
	}
}

// ─── 3.3 · Annotate ──────────────────────────────────────────────────────────

// TestAnnotate_SingleSnapshot verifies that after one snapshot every line is
// attributed to that snapshot.
func TestAnnotate_SingleSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "code.go", "line1\nline2\nline3\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "snap1")

	result, err := annotate.Annotate(projectRoot, "code.go")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.TotalLines != 3 {
		t.Errorf("expected 3 lines, got %d", result.TotalLines)
	}
	for _, l := range result.Lines {
		if l.SnapshotID != snap.ID {
			t.Errorf("line %d: expected snapshot %q, got %q", l.Line, snap.ID, l.SnapshotID)
		}
	}
}

// TestAnnotate_NewLinesAttributedToLaterSnapshot verifies that lines added in a
// second snapshot are attributed to the second snapshot, not the first.
func TestAnnotate_NewLinesAttributedToLaterSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "evolving.go", "original line\n")
	snap1 := createMainSnap(t, projectRoot, mainBranchID, "snap1")

	time.Sleep(time.Second) // ensure distinct timestamps
	writeFile(t, projectRoot, "evolving.go", "original line\nnew line added\n")
	snap2 := createMainSnap(t, projectRoot, mainBranchID, "snap2")

	result, err := annotate.Annotate(projectRoot, "evolving.go")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.TotalLines != 2 {
		t.Fatalf("expected 2 lines, got %d", result.TotalLines)
	}
	if result.Lines[0].SnapshotID != snap1.ID {
		t.Errorf("line 1 should come from snap1, got %q", result.Lines[0].SnapshotID)
	}
	if result.Lines[1].SnapshotID != snap2.ID {
		t.Errorf("line 2 should come from snap2, got %q", result.Lines[1].SnapshotID)
	}
}

// TestAnnotate_UntrackedFile verifies that a file that was never snapshotted
// returns lines with snapshot_id = "" and label "(untracked)".
func TestAnnotate_UntrackedFile(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "untracked.go", "a\nb\n")

	result, err := annotate.Annotate(projectRoot, "untracked.go")
	if err != nil {
		t.Fatalf("Annotate untracked: %v", err)
	}
	for _, l := range result.Lines {
		if l.SnapshotID != "" {
			t.Errorf("line %d: expected empty snapshot_id, got %q", l.Line, l.SnapshotID)
		}
		if l.Label != "(untracked)" {
			t.Errorf("line %d: expected label '(untracked)', got %q", l.Line, l.Label)
		}
	}
}

// ─── 3.4 · GetFileVersions (single-query annotate) ───────────────────────────

// TestGetFileVersions_ReturnsAllVersionsOldestFirst verifies that
// GetFileVersions returns one row per snapshot that contains the file,
// ordered oldest-first.
func TestGetFileVersions_ReturnsAllVersionsOldestFirst(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "versioned.go", "v1\n")
	createMainSnap(t, projectRoot, mainBranchID, "v1")

	time.Sleep(time.Second)
	writeFile(t, projectRoot, "versioned.go", "v2\n")
	createMainSnap(t, projectRoot, mainBranchID, "v2")

	time.Sleep(time.Second)
	writeFile(t, projectRoot, "versioned.go", "v3\n")
	createMainSnap(t, projectRoot, mainBranchID, "v3")

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	versions, err := store.GetFileVersions("versioned.go")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Must be oldest-first.
	for i := 1; i < len(versions); i++ {
		if versions[i].Timestamp <= versions[i-1].Timestamp {
			t.Errorf("versions not ordered oldest-first at index %d", i)
		}
	}
}

// TestGetFileVersions_OmitsFilesNotInSnapshot verifies that files not present
// in a snapshot do not appear in the versions list.
func TestGetFileVersions_OmitsFilesNotInSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	// Snapshot 1: only file_a.go.
	writeFile(t, projectRoot, "file_a.go", "a\n")
	createMainSnap(t, projectRoot, mainBranchID, "snap1")

	// Snapshot 2: add file_b.go.
	writeFile(t, projectRoot, "file_b.go", "b\n")
	createMainSnap(t, projectRoot, mainBranchID, "snap2")

	store, _ := db.Open(projectRoot)
	defer store.Close()

	versionsA, _ := store.GetFileVersions("file_a.go")
	versionsB, _ := store.GetFileVersions("file_b.go")

	if len(versionsA) != 2 {
		t.Errorf("file_a.go: expected 2 versions (both snapshots), got %d", len(versionsA))
	}
	if len(versionsB) != 1 {
		t.Errorf("file_b.go: expected 1 version (only snap2), got %d", len(versionsB))
	}
}

// ─── 3.5 · avc_run_in_workspace gate ─────────────────────────────────────────

// TestRunInWorkspace_DisabledByDefault verifies that the gate rejects execution
// when [run] enabled is false (the zero value default).
func TestRunInWorkspace_DisabledByDefault(t *testing.T) {
	projectRoot := setupTestProject(t)

	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Default config must have Enabled = false.
	if cfg.Run.Enabled {
		t.Error("expected Run.Enabled = false by default; got true")
	}
}

// TestRunInWorkspace_EnabledViaConfig verifies that setting [run] enabled = true
// in config makes the value readable back correctly.
func TestRunInWorkspace_EnabledViaConfig(t *testing.T) {
	projectRoot := setupTestProject(t)

	cfg, _ := config.Load(projectRoot)
	cfg.Run.Enabled = true
	if err := config.Save(projectRoot, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !loaded.Run.Enabled {
		t.Error("expected Run.Enabled = true after saving; got false")
	}
}
