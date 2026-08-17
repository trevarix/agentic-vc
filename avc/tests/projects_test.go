// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for AVC project discovery, which is how a host with no working
// directory (Claude Desktop) finds the projects a user can act on.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/projects"
)

// mkProject creates a directory containing a .avc marker, standing in for an
// initialized project without paying for a real one.
func mkProject(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".avc"), 0755); err != nil {
		t.Fatalf("create project %s: %v", path, err)
	}
	return path
}

// names extracts project names for order-sensitive assertions.
func names(found []projects.Project) []string {
	out := make([]string, 0, len(found))
	for _, p := range found {
		out = append(out, p.Name)
	}
	return out
}

// TestDiscoverFindsNestedProjectsSorted covers the ordinary case: a folder of
// repositories, some one level down and some deeper.
func TestDiscoverFindsNestedProjectsSorted(t *testing.T) {
	root := t.TempDir()
	mkProject(t, filepath.Join(root, "zebra"))
	mkProject(t, filepath.Join(root, "alpha"))
	mkProject(t, filepath.Join(root, "work", "api"))

	got := names(projects.Discover([]string{root}))
	want := []string{"alpha", "api", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestDiscoverDoesNotDescendIntoProjects guards against workspaces appearing
// as projects. A branch workspace under .avc/workspaces/ is a copy of the
// project and would otherwise be offered to the user as a separate one.
func TestDiscoverDoesNotDescendIntoProjects(t *testing.T) {
	root := t.TempDir()
	proj := mkProject(t, filepath.Join(root, "myapp"))
	mkProject(t, filepath.Join(proj, ".avc", "workspaces", "feat-x"))
	mkProject(t, filepath.Join(proj, "nested"))

	got := names(projects.Discover([]string{root}))
	if len(got) != 1 || got[0] != "myapp" {
		t.Errorf("found %v, want exactly [myapp]", got)
	}
}

// TestDiscoverSkipsHeavyAndHiddenDirs keeps discovery fast on real trees.
func TestDiscoverSkipsHeavyAndHiddenDirs(t *testing.T) {
	root := t.TempDir()
	mkProject(t, filepath.Join(root, "node_modules", "pkg"))
	mkProject(t, filepath.Join(root, "vendor", "dep"))
	mkProject(t, filepath.Join(root, ".cache", "thing"))
	mkProject(t, filepath.Join(root, "real"))

	got := names(projects.Discover([]string{root}))
	if len(got) != 1 || got[0] != "real" {
		t.Errorf("found %v, want exactly [real]", got)
	}
}

// TestDiscoverRootItselfIsAProject covers pointing AVC straight at a project
// rather than at a folder of them.
func TestDiscoverRootItselfIsAProject(t *testing.T) {
	root := mkProject(t, t.TempDir())

	got := projects.Discover([]string{root})
	if len(got) != 1 {
		t.Fatalf("found %d projects, want 1", len(got))
	}
	if got[0].Path != root {
		t.Errorf("path %q, want %q", got[0].Path, root)
	}
}

// TestDiscoverDedupesOverlappingRoots covers a user configuring both a parent
// and a child folder, which should not list the same project twice.
func TestDiscoverDedupesOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	proj := mkProject(t, filepath.Join(root, "app"))

	got := projects.Discover([]string{root, proj})
	if len(got) != 1 {
		t.Errorf("found %d projects, want 1 (dedupe failed): %v", len(got), names(got))
	}
}

// TestDiscoverToleratesMissingRoot ensures one bad path does not lose the
// projects under the good ones.
func TestDiscoverToleratesMissingRoot(t *testing.T) {
	root := t.TempDir()
	mkProject(t, filepath.Join(root, "app"))

	got := names(projects.Discover([]string{filepath.Join(root, "does-not-exist"), root}))
	if len(got) != 1 || got[0] != "app" {
		t.Errorf("found %v, want exactly [app]", got)
	}
}

// TestDiscoverRespectsDepthLimit documents the bound. A project buried deeper
// than the limit is not found, which is the cost of keeping large trees fast.
func TestDiscoverRespectsDepthLimit(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f", "buried")
	mkProject(t, deep)

	if got := projects.Discover([]string{root}); len(got) != 0 {
		t.Errorf("found %v past the depth limit, want none", names(got))
	}
}
