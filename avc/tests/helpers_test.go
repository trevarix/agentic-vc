package tests

import (
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

// setupProjectWithMain initializes a project and ensures the main branch record
// exists in the DB (EnsureMainBranch is idempotent).
// Returns the project root and the main branch ID, which callers must pass when
// creating snapshots intended for the main branch.
func setupProjectWithMain(t *testing.T) (projectRoot, mainBranchID string) {
	t.Helper()
	projectRoot = setupTestProject(t)

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	mainBranch, err := store.EnsureMainBranch(proj.ID)
	if err != nil {
		t.Fatalf("EnsureMainBranch: %v", err)
	}

	return projectRoot, mainBranch.ID
}

// createMainSnap creates a snapshot associated with the main branch.
func createMainSnap(t *testing.T, projectRoot, mainBranchID, label string) *snapshot.Result {
	t.Helper()
	snap, err := snapshot.Create(projectRoot, label, "", "", mainBranchID, "")
	if err != nil {
		t.Fatalf("create main snapshot %q: %v", label, err)
	}
	return snap
}
