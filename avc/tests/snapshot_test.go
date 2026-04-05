package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

func TestSnapshot_Create_BasicSnapshot(t *testing.T) {
	projectRoot := setupTestProject(t)

	writeFile(t, projectRoot, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, projectRoot, "README.md", "# My Project\n")

	result, err := snapshot.Create(projectRoot, "initial", "test-agent", "first snapshot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Label != "initial" {
		t.Errorf("label = %q, want %q", result.Label, "initial")
	}
	if result.FileCount != 2 {
		t.Errorf("file count = %d, want 2", result.FileCount)
	}
	if result.TotalSize == 0 {
		t.Error("expected non-zero total size")
	}
}

func TestSnapshot_Create_IgnoresAVCDirectory(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "package app\n")
	// Write a file inside .avc/ — it must not be included.
	writeFile(t, projectRoot, ".avc/secret.txt", "internal")

	result, err := snapshot.Create(projectRoot, "test", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FileCount != 1 {
		t.Errorf("file count = %d, want 1 (.avc/ files must be excluded)", result.FileCount)
	}
}

func TestSnapshot_Create_PersistsToDatabase(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "file.txt", "hello")

	result, err := snapshot.Create(projectRoot, "persisted", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	snap, err := store.GetSnapshot(result.ID)
	if err != nil {
		t.Fatalf("snapshot not found in db: %v", err)
	}
	if snap.Label != "persisted" {
		t.Errorf("db label = %q, want %q", snap.Label, "persisted")
	}
}

func TestSnapshot_Create_RequiresInitializedProject(t *testing.T) {
	dir := t.TempDir()
	_, err := snapshot.Create(dir, "label", "", "")
	if err == nil {
		t.Error("expected error for uninitialized project, got nil")
	}
}

// setupTestProject creates a temp directory, initializes an AVC project, and
// returns the project root path.
func setupTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := db.InitProject(root); err != nil {
		t.Fatalf("init project: %v", err)
	}
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
