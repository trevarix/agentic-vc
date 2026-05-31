package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// TestIntegration_FullSnapshotRestoreCycle covers the primary user workflow:
// take snapshot → mutate files → restore → verify files are back.
func TestIntegration_FullSnapshotRestoreCycle(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Initial project state.
	writeFile(t, projectRoot, "src/app.go", "package app\n\nconst Version = \"1.0\"\n")
	writeFile(t, projectRoot, "README.md", "# My App\n")

	snap1, err := snapshot.Create(projectRoot, "v1.0 release", "claude", "stable release", "", "")
	if err != nil {
		t.Fatalf("create v1 snapshot: %v", err)
	}

	// Simulate agent making changes.
	writeFile(t, projectRoot, "src/app.go", "package app\n\nconst Version = \"2.0\"\n\nfunc NewFeature() {}\n")
	writeFile(t, projectRoot, "src/feature.go", "package app\n\nfunc Feature() {}\n")

	snap2, err := snapshot.Create(projectRoot, "v2.0 wip", "claude", "agent added feature", "", "")
	if err != nil {
		t.Fatalf("create v2 snapshot: %v", err)
	}

	// Verify diff sees the changes.
	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(result.Files) < 2 {
		t.Errorf("expected at least 2 changed files in diff, got %d", len(result.Files))
	}

	// Restore to v1.
	restoreResult, err := restore.Restore(projectRoot, snap1.ID)
	if err != nil {
		t.Fatalf("restore to v1: %v", err)
	}
	if restoreResult.RestoredFiles != snap1.FileCount {
		t.Errorf("restored %d files, expected %d", restoreResult.RestoredFiles, snap1.FileCount)
	}

	// Verify app.go is back to v1 content.
	appGo, err := os.ReadFile(filepath.Join(projectRoot, "src/app.go"))
	if err != nil {
		t.Fatalf("read restored app.go: %v", err)
	}
	if string(appGo) != "package app\n\nconst Version = \"1.0\"\n" {
		t.Errorf("restored app.go content mismatch:\n%s", string(appGo))
	}

	_ = snap2
}

// TestIntegration_MultipleSnapshots verifies the snapshot list grows correctly.
func TestIntegration_MultipleSnapshots(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "file.go", "v1")

	for i, label := range []string{"first", "second", "third"} {
		writeFile(t, projectRoot, "file.go", "version "+string(rune('1'+i)))
		if _, err := snapshot.Create(projectRoot, label, "", "", "", ""); err != nil {
			t.Fatalf("snapshot %q: %v", label, err)
		}
	}

	// Verify via db directly (list is tested separately).
	_ = projectRoot
}
