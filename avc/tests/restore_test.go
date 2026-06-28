// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
)

func TestRestore_RollsBackModifiedFile(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "version 1")

	snap, err := snapshot.Create(projectRoot, "v1", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	// Modify the file after taking the snapshot.
	writeFile(t, projectRoot, "app.go", "version 2")

	if _, err := restore.Restore(projectRoot, snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "version 1" {
		t.Errorf("restored content = %q, want %q", string(data), "version 1")
	}
}

func TestRestore_ReturnsCorrectFileCounts(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "a.txt", "a")
	writeFile(t, projectRoot, "b.txt", "b")

	snap, err := snapshot.Create(projectRoot, "two-files", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	result, err := restore.Restore(projectRoot, snap.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if result.RestoredFiles != 2 {
		t.Errorf("restored files = %d, want 2", result.RestoredFiles)
	}
}

func TestRestore_ErrorOnUnknownSnapshotID(t *testing.T) {
	projectRoot := setupTestProject(t)

	_, err := restore.Restore(projectRoot, "snap-doesnotexist")
	if err == nil {
		t.Error("expected error for unknown snapshot ID, got nil")
	}
}
