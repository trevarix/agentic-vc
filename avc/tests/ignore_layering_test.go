// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests that a branch snapshot layers the project-root .avcignore (read fresh)
// underneath the workspace's own, so a root ignore edit takes effect on live
// branches while workspace-specific additions still apply.
package tests

import (
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// snapshotHasPath reports whether a snapshot tracks the given relative path.
func snapshotHasPath(t *testing.T, projectRoot, snapID, rel string) bool {
	t.Helper()
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	files, err := store.GetSnapshotFiles(snapID)
	if err != nil {
		t.Fatalf("GetSnapshotFiles: %v", err)
	}
	for _, f := range files {
		if f.RelativePath == rel {
			return true
		}
	}
	return false
}

func TestIgnoreLayering_RootRuleAppliesToLiveBranch(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	// Root .avcignore starts empty of custom rules.
	writeFile(t, projectRoot, ".avcignore", "# project ignores\n")
	writeFile(t, projectRoot, "app.go", "package main\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/layer", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// After branch creation, edit the ROOT .avcignore to add a new rule, and
	// add a WORKSPACE-only rule. Create files matching each in the workspace.
	writeFile(t, projectRoot, ".avcignore", "# project ignores\nlogs/\n")
	writeFile(t, ws, ".avcignore", "# project ignores\nmedia/\n")
	writeFile(t, ws, "logs/run.txt", "log output")   // matches the fresh ROOT rule
	writeFile(t, ws, "media/pic.bin", "bytes")        // matches the WORKSPACE rule
	writeFile(t, ws, "keep.go", "package main\n")      // tracked

	snap, err := snapshot.Create(projectRoot, "layered", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snapshotHasPath(t, projectRoot, snap.ID, "logs/run.txt") {
		t.Error("root .avcignore edit (logs/) should apply to the live branch snapshot")
	}
	if snapshotHasPath(t, projectRoot, snap.ID, "media/pic.bin") {
		t.Error("workspace .avcignore rule (media/) should apply")
	}
	if !snapshotHasPath(t, projectRoot, snap.ID, "keep.go") {
		t.Error("a non-ignored file must still be tracked")
	}
}
