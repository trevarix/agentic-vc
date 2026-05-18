package tests

import (
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
)

// TestBranch_Create_RefusesMainName verifies that creating a branch named
// "main" returns an error.
func TestBranch_Create_RefusesMainName(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)

	_, err := branch.Create(projectRoot, "main", "")
	if err == nil {
		t.Error("expected error when creating branch named 'main', got nil")
	}
}

// TestBranch_Create_UsesLatestMainSnapshotAsBase verifies that when no base
// snapshot is specified, the branch is created from the HEAD of main.
func TestBranch_Create_UsesLatestMainSnapshotAsBase(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "main-snap")

	b, err := branch.Create(projectRoot, "feat/auto-base", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if b.BaseSnapshotID != snap.ID {
		t.Errorf("BaseSnapshotID = %q, want %q", b.BaseSnapshotID, snap.ID)
	}
}

// TestBranch_Create_WithExplicitBase verifies that passing a specific snapshot
// ID sets the branch's base to that snapshot.
func TestBranch_Create_WithExplicitBase(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap1 := createMainSnap(t, projectRoot, mainBranchID, "snap1")

	writeFile(t, projectRoot, "file.go", "v2")
	createMainSnap(t, projectRoot, mainBranchID, "snap2")

	// Use snap1 explicitly as the base.
	b, err := branch.Create(projectRoot, "feat/explicit-base", snap1.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if b.BaseSnapshotID != snap1.ID {
		t.Errorf("BaseSnapshotID = %q, want %q", b.BaseSnapshotID, snap1.ID)
	}
}

// TestBranch_List_IncludesAllBranches verifies that List returns all created
// branches including main.
func TestBranch_List_IncludesAllBranches(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	branchNames := []string{"feat/a", "feat/b", "feat/c"}
	for _, name := range branchNames {
		if _, err := branch.Create(projectRoot, name, snap.ID); err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
	}

	branches, err := branch.List(projectRoot)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	nameSet := make(map[string]bool)
	for _, b := range branches {
		nameSet[b.Name] = true
	}

	if !nameSet["main"] {
		t.Error("expected 'main' in branch list")
	}
	for _, name := range branchNames {
		if !nameSet[name] {
			t.Errorf("expected %q in branch list", name)
		}
	}
}

// TestBranch_Switch_UpdatesActiveBranchInConfig verifies that Switch changes
// the active branch stored in config.toml.
func TestBranch_Switch_UpdatesActiveBranchInConfig(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	_, err := branch.Create(projectRoot, "feat/switch-test", snap.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := branch.Switch(projectRoot, "feat/switch-test"); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Branch.Active != "feat/switch-test" {
		t.Errorf("active branch = %q, want %q", cfg.Branch.Active, "feat/switch-test")
	}
}

// TestBranch_Switch_ToMain verifies that switching back to main works.
func TestBranch_Switch_ToMain(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	_, err := branch.Create(projectRoot, "feat/side", snap.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := branch.Switch(projectRoot, "feat/side"); err != nil {
		t.Fatalf("Switch to feat/side: %v", err)
	}

	if err := branch.Switch(projectRoot, "main"); err != nil {
		t.Fatalf("Switch to main: %v", err)
	}

	cfg, _ := config.Load(projectRoot)
	if cfg.Branch.Active != "main" {
		t.Errorf("active branch = %q, want %q", cfg.Branch.Active, "main")
	}
}

// TestBranch_Delete_RefusesMain verifies that deleting the "main" branch
// returns an error.
func TestBranch_Delete_RefusesMain(t *testing.T) {
	projectRoot := setupTestProject(t)

	if err := branch.Delete(projectRoot, "main"); err == nil {
		t.Error("expected error when deleting 'main' branch, got nil")
	}
}

// TestBranch_Delete_RefusesActiveBranch verifies that deleting the currently
// active non-main branch returns an error.
func TestBranch_Delete_RefusesActiveBranch(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", "v1")
	snap := createMainSnap(t, projectRoot, mainBranchID, "base")

	_, err := branch.Create(projectRoot, "feat/active", snap.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := branch.Switch(projectRoot, "feat/active"); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	if err := branch.Delete(projectRoot, "feat/active"); err == nil {
		t.Error("expected error when deleting the active branch, got nil")
	}
}

// TestBranch_Delete_NonExistentBranch verifies that deleting a branch that
// does not exist returns an error.
func TestBranch_Delete_NonExistentBranch(t *testing.T) {
	projectRoot := setupTestProject(t)

	if err := branch.Delete(projectRoot, "does-not-exist"); err == nil {
		t.Error("expected error when deleting non-existent branch, got nil")
	}
}

// TestBranch_GetActiveBranchName_DefaultsToMain verifies that a freshly
// initialized project reports "main" as the active branch.
func TestBranch_GetActiveBranchName_DefaultsToMain(t *testing.T) {
	projectRoot := setupTestProject(t)

	name := branch.GetActiveBranchName(projectRoot)
	if name != "main" {
		t.Errorf("active branch = %q, want %q", name, "main")
	}
}

// TestBranch_WorkspacePath_NonMainReturnsNonEmpty verifies that a non-main
// branch has a non-empty workspace path.
func TestBranch_WorkspacePath_NonMainReturnsNonEmpty(t *testing.T) {
	ws := branch.WorkspacePath("/proj", "feat/x")
	if ws == "" {
		t.Error("expected non-empty workspace path for non-main branch")
	}
}

// TestBranch_WorkspacePath_MainReturnsEmpty verifies the main branch contract.
func TestBranch_WorkspacePath_MainReturnsEmpty(t *testing.T) {
	if ws := branch.WorkspacePath("/proj", "main"); ws != "" {
		t.Errorf("main branch workspace path = %q, want empty", ws)
	}
}
