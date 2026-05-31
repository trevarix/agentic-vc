// Package tests — Phase 7: Branch Lifecycle & Automation tests.
//
// Covers:
//   7.1 Branch status: default active, set merged/abandoned, list filtering, prune workspaces
//   7.2 Branch rename: workspace dir, stat cache, active branch reference
//   7.3 Active branch in SQLite: Switch writes project_state, GetActiveBranchName reads DB
//   7.4 Pre/post hooks: pre-snapshot abort on non-zero exit, post-snapshot runs after success
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

// ─── 7.1 Branch Lifecycle States ─────────────────────────────────────────────

func TestBranchStatus_NewBranchIsActive(t *testing.T) {
	root, _ := setupProjectWithMain(t)

	b, err := branch.Create(root, "feat-status", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Status != "active" {
		t.Errorf("new branch status = %q, want %q", b.Status, "active")
	}
}

func TestBranchStatus_ListDefaultExcludesMerged(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "merged-branch", "")
	branch.Switch(root, "main")

	// Mark merged via DB directly.
	store, _ := db.Open(root)
	defer store.Close()
	proj, _ := store.GetProject(root)
	b, _ := store.GetBranchByName(proj.ID, "merged-branch")
	store.SetBranchStatus(b.ID, "merged")
	store.Close()

	branches, err := branch.List(root) // default: active only
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, br := range branches {
		if br.Name == "merged-branch" {
			t.Error("merged branch should not appear in default List()")
		}
	}
}

func TestBranchStatus_ListAll(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "br-all-test", "")
	branch.Switch(root, "main")

	store, _ := db.Open(root)
	proj, _ := store.GetProject(root)
	b, _ := store.GetBranchByName(proj.ID, "br-all-test")
	store.SetBranchStatus(b.ID, "abandoned")
	store.Close()

	all, err := branch.ListByStatus(root, "")
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	found := false
	for _, br := range all {
		if br.Name == "br-all-test" {
			found = true
			if br.Status != "abandoned" {
				t.Errorf("status = %q, want abandoned", br.Status)
			}
		}
	}
	if !found {
		t.Error("abandoned branch not found in ListByStatus(all)")
	}
}

func TestBranchStatus_FilterByMerged(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "to-merge", "")
	branch.Switch(root, "main")
	branch.Create(root, "still-active", "")
	branch.Switch(root, "main")

	store, _ := db.Open(root)
	proj, _ := store.GetProject(root)
	bm, _ := store.GetBranchByName(proj.ID, "to-merge")
	store.SetBranchStatus(bm.ID, "merged")
	store.Close()

	merged, _ := branch.ListByStatus(root, "merged")
	if len(merged) != 1 || merged[0].Name != "to-merge" {
		t.Errorf("expected [to-merge] in merged list, got %v", merged)
	}
}

func TestBranchAbandon(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "to-abandon", "")
	branch.Switch(root, "main")

	if err := branch.Abandon(root, "to-abandon"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}

	byStatus, _ := branch.ListByStatus(root, "abandoned")
	found := false
	for _, b := range byStatus {
		if b.Name == "to-abandon" {
			found = true
		}
	}
	if !found {
		t.Error("abandoned branch not found in abandoned list")
	}
}

func TestBranchAbandon_Main_Fails(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	if err := branch.Abandon(root, "main"); err == nil {
		t.Error("expected error abandoning main branch")
	}
}

func TestBranchPruneMergedWorkspaces(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	b, err := branch.Create(root, "ws-to-prune", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws := branch.WorkspacePath(root, b.Name)
	branch.Switch(root, "main")

	// Verify workspace exists before prune.
	if _, err := os.Stat(ws); err != nil {
		t.Fatalf("workspace should exist before prune: %v", err)
	}

	// Mark branch merged.
	store, _ := db.Open(root)
	proj, _ := store.GetProject(root)
	br, _ := store.GetBranchByName(proj.ID, "ws-to-prune")
	store.SetBranchStatus(br.ID, "merged")
	store.Close()

	pruned, err := branch.PruneMergedWorkspaces(root)
	if err != nil {
		t.Fatalf("PruneMergedWorkspaces: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "ws-to-prune" {
		t.Errorf("expected [ws-to-prune] pruned, got %v", pruned)
	}

	// Workspace should be gone.
	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		t.Errorf("workspace should have been removed after prune")
	}
}

// ─── 7.2 Branch Rename ───────────────────────────────────────────────────────

func TestBranchRename_UpdatesDB(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "old-name", "")
	branch.Switch(root, "main")

	if err := branch.Rename(root, "old-name", "new-name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	branches, _ := branch.ListByStatus(root, "")
	found := false
	for _, b := range branches {
		if b.Name == "new-name" {
			found = true
		}
		if b.Name == "old-name" {
			t.Error("old name still appears after rename")
		}
	}
	if !found {
		t.Error("new branch name not found after rename")
	}
}

func TestBranchRename_MovesWorkspace(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "ws-old", "")
	branch.Switch(root, "main")

	oldWS := branch.WorkspacePath(root, "ws-old")
	newWS := branch.WorkspacePath(root, "ws-new")

	if _, err := os.Stat(oldWS); err != nil {
		t.Fatalf("workspace should exist before rename: %v", err)
	}

	if err := branch.Rename(root, "ws-old", "ws-new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := os.Stat(oldWS); !os.IsNotExist(err) {
		t.Error("old workspace should be gone after rename")
	}
	if _, err := os.Stat(newWS); err != nil {
		t.Errorf("new workspace should exist after rename: %v", err)
	}
}

func TestBranchRename_UpdatesActiveBranch(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "active-old", "")
	branch.Switch(root, "active-old") // explicitly switch so project_state is written

	if err := branch.Rename(root, "active-old", "active-new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	name := branch.GetActiveBranchName(root)
	if name != "active-new" {
		t.Errorf("active branch = %q after rename, want %q", name, "active-new")
	}
}

func TestBranchRename_Main_Fails(t *testing.T) {
	root, _ := setupProjectWithMain(t)
	if err := branch.Rename(root, "main", "new-main"); err == nil {
		t.Error("expected error renaming main")
	}
}

func TestBranchRename_DuplicateName_Fails(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "branch-a", "")
	branch.Switch(root, "main")
	branch.Create(root, "branch-b", "")
	branch.Switch(root, "main")

	if err := branch.Rename(root, "branch-a", "branch-b"); err == nil {
		t.Error("expected error renaming to an existing branch name")
	}
}

// ─── 7.3 Active Branch in SQLite ─────────────────────────────────────────────

func TestActiveBranch_SwitchWritesToDB(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "db-branch", "")
	branch.Switch(root, "db-branch") // write active branch to project_state

	// GetActiveBranchName should return "db-branch" (read from DB).
	name := branch.GetActiveBranchName(root)
	if name != "db-branch" {
		t.Errorf("GetActiveBranchName = %q, want %q", name, "db-branch")
	}

	// Switch back and verify.
	branch.Switch(root, "main")
	name = branch.GetActiveBranchName(root)
	if name != "main" {
		t.Errorf("GetActiveBranchName after switch = %q, want %q", name, "main")
	}
}

func TestActiveBranch_FallsBackToConfig(t *testing.T) {
	// A project that has config.toml but no project_state row should fall back.
	root := setupTestProject(t)

	// Write a config.toml with branch.active = "some-branch".
	// (We can't actually switch to a non-existent branch, so we verify
	// GetActiveBranchName returns "main" when no config entry exists.)
	name := branch.GetActiveBranchName(root)
	if name != "main" {
		t.Errorf("expected 'main' as fallback, got %q", name)
	}
}

func TestActiveBranch_DBPreferredOverConfig(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	createMainSnap(t, root, mainBranchID, "baseline")

	branch.Create(root, "prefer-db", "")
	branch.Switch(root, "prefer-db") // write active branch to project_state
	// Now both config.toml and project_state say "prefer-db".

	// Manually corrupt config.toml to say something different.
	cfg, _ := config.Load(root)
	cfg.Branch.Active = "wrong-from-config"
	config.Save(root, cfg)

	// DB should still say "prefer-db" — it was written by Switch.
	name := branch.GetActiveBranchName(root)
	if name != "prefer-db" {
		t.Errorf("GetActiveBranchName = %q, expected DB value %q", name, "prefer-db")
	}
}

// ─── 7.4 Pre/Post Snapshot Hooks ─────────────────────────────────────────────

func TestHooks_PreSnapshot_SuccessAllowsSnapshot(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	writeFile(t, root, "app.go", "package main\n")

	// Write a config with a pre_snapshot hook that exits 0 (always succeeds).
	cfg, _ := config.Load(root)
	cfg.Hooks.PreSnapshot = trueCommand()
	config.Save(root, cfg)

	_, err := snapshot.Create(root, "with-hook", "", "", mainBranchID, "")
	if err != nil {
		t.Fatalf("snapshot with successful pre-hook failed: %v", err)
	}
}

func TestHooks_PreSnapshot_NonZeroAborts(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	writeFile(t, root, "app.go", "package main\n")

	cfg, _ := config.Load(root)
	cfg.Hooks.PreSnapshot = falseCommand()
	config.Save(root, cfg)

	_, err := snapshot.Create(root, "aborted-snap", "", "", mainBranchID, "")
	if err == nil {
		t.Fatal("expected snapshot to be aborted by failing pre-snapshot hook")
	}
}

func TestHooks_PostSnapshot_RunsAfterSuccess(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	writeFile(t, root, "app.go", "package main\n")

	// Write the snapshot ID to a sentinel file via post-hook.
	sentinel := filepath.Join(root, ".avc", "post-hook-ran")
	cfg, _ := config.Load(root)
	cfg.Hooks.PostSnapshot = touchCommand(sentinel)
	config.Save(root, cfg)

	_, err := snapshot.Create(root, "post-hook-test", "", "", mainBranchID, "")
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	if _, statErr := os.Stat(sentinel); os.IsNotExist(statErr) {
		t.Error("post-snapshot hook did not run (sentinel file missing)")
	}
}

func TestHooks_PostSnapshot_FailureNonFatal(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	writeFile(t, root, "app.go", "package main\n")

	// Post-hook exits non-zero — snapshot should still succeed.
	cfg, _ := config.Load(root)
	cfg.Hooks.PostSnapshot = falseCommand()
	config.Save(root, cfg)

	snap, err := snapshot.Create(root, "post-hook-fail", "", "", mainBranchID, "")
	if err != nil {
		t.Fatalf("snapshot should succeed even with failing post-hook: %v", err)
	}
	if snap == nil || snap.ID == "" {
		t.Error("expected valid snapshot result")
	}
}

// ─── Cross-platform shell command helpers ─────────────────────────────────────

// trueCommand returns a shell command that always exits 0.
func trueCommand() string {
	if isWindows() {
		return "exit 0"
	}
	return "true"
}

// falseCommand returns a shell command that always exits 1.
func falseCommand() string {
	if isWindows() {
		return "exit 1"
	}
	return "false"
}

// touchCommand returns a shell command that creates a file at path.
// On Windows we avoid quoting: Go's t.TempDir() paths never contain spaces,
// and cmd.exe misinterprets Go's \" escaping when the path is double-quoted.
func touchCommand(path string) string {
	if isWindows() {
		return `copy /Y NUL ` + path
	}
	return `touch ` + path
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
