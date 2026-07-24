// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests: adding a path to .avcignore mid-branch must not untrack
// files that still exist on disk. git's rule — .gitignore never untracks a
// tracked file; only an explicit removal does. Before the fix, the newly
// ignored files dropped out of the snapshot, a branch diff reported them
// [deleted], and a merge would have deleted the real files from the project
// root.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

func TestUntrack_IgnoreAddedMidBranch_KeepsPresentFilesTracked(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "media/photo1.jpg", "real image 1")
	writeFile(t, projectRoot, "media/photo2.jpg", "real image 2")
	writeFile(t, projectRoot, "app.go", "package main\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/ignore-media", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// A real feature edit, plus a mid-branch ignore addition for media/.
	writeFile(t, ws, "app.go", "package main\n\nfunc main() {}\n")
	writeFile(t, ws, ".avcignore", "media/\n")

	head, err := snapshot.Create(projectRoot, "edit + ignore media", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// The media files, still present on disk, must remain tracked — not deleted.
	res, err := diff.Compare(projectRoot, base.ID, head.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, f := range res.Files {
		if f.Type == diff.Deleted && filepath.Dir(f.Path) == "media" {
			t.Errorf("%s reported as deleted, but it still exists on disk", f.Path)
		}
	}

	// End to end: a merge must NOT delete the real media files from the root.
	result, err := merge.Merge(projectRoot, "feat/ignore-media")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts != 0 {
		t.Errorf("Conflicts = %d, want 0", result.Conflicts)
	}
	for _, f := range []string{"media/photo1.jpg", "media/photo2.jpg"} {
		if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s must survive the merge, stat err = %v", f, err)
		}
	}
	// The real feature edit still merged.
	got, _ := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if string(got) != "package main\n\nfunc main() {}\n" {
		t.Errorf("app.go edit did not merge: %q", got)
	}
}

// TestUntrack_GenuinelyDeletedFileStillDeletes verifies the fix does not
// over-reach: a file actually removed from disk is still a real deletion, even
// if an ignore rule would also match it.
func TestUntrack_GenuinelyDeletedFileStillDeletes(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "media/gone.jpg", "bytes")
	writeFile(t, projectRoot, "app.go", "package main\n")
	base := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/del", base.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// Ignore media/ AND actually delete the file from disk.
	writeFile(t, ws, ".avcignore", "media/\n")
	if err := os.Remove(filepath.Join(ws, "media", "gone.jpg")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	head, err := snapshot.Create(projectRoot, "deleted gone.jpg", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	res, err := diff.Compare(projectRoot, base.ID, head.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var sawDelete bool
	for _, f := range res.Files {
		if f.Type == diff.Deleted && f.Path == "media/gone.jpg" {
			sawDelete = true
		}
	}
	if !sawDelete {
		t.Error("a file genuinely removed from disk should still be reported deleted")
	}
}
