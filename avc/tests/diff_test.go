package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

func TestDiff_DetectsModifiedFile(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "main.go", "package main\n")

	snap1, err := snapshot.Create(projectRoot, "before", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}

	writeFile(t, projectRoot, "main.go", "package main\n\nfunc main() {}\n")

	snap2, err := snapshot.Create(projectRoot, "after", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.Files))
	}
	if result.Files[0].Type != diff.Modified {
		t.Errorf("change type = %q, want %q", result.Files[0].Type, diff.Modified)
	}
	if result.Files[0].Path != "main.go" {
		t.Errorf("path = %q, want %q", result.Files[0].Path, "main.go")
	}
}

func TestDiff_DetectsAddedFile(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "existing.go", "package p\n")

	snap1, _ := snapshot.Create(projectRoot, "before", "", "", "", "")
	writeFile(t, projectRoot, "new.go", "package p\n")
	snap2, _ := snapshot.Create(projectRoot, "after", "", "", "", "")

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	var found bool
	for _, f := range result.Files {
		if f.Path == "new.go" && f.Type == diff.Added {
			found = true
		}
	}
	if !found {
		t.Error("expected new.go to be listed as Added")
	}
}

func TestDiff_DetectsDeletedFile(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "to-delete.go", "package p\n")

	snap1, _ := snapshot.Create(projectRoot, "before", "", "", "", "")

	removeTestFile(t, projectRoot, "to-delete.go")
	snap2, _ := snapshot.Create(projectRoot, "without file", "", "", "", "")

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	var found bool
	for _, f := range result.Files {
		if f.Path == "to-delete.go" && f.Type == diff.Deleted {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to-delete.go to be listed as Deleted; got: %v", result.Files)
	}
	_ = snap2
}

func TestDiff_ErrorOnUnknownSnapshot(t *testing.T) {
	projectRoot := setupTestProject(t)

	_, err := diff.Compare(projectRoot, "snap-bad1", "snap-bad2")
	if err == nil {
		t.Error("expected error for unknown snapshot IDs, got nil")
	}
}

func TestDiff_NoChangesReturnsEmptyList(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "same.go", "unchanged")

	snap1, _ := snapshot.Create(projectRoot, "s1", "", "", "", "")
	snap2, _ := snapshot.Create(projectRoot, "s2", "", "", "", "")

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(result.Files) != 0 {
		t.Errorf("expected 0 changed files, got %d", len(result.Files))
	}
}

func removeTestFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatalf("remove file: %v", err)
	}
}
