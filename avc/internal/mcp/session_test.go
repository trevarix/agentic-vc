// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkProj creates a directory with a .avc marker, standing in for an
// initialized project.
func mkProj(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".avc"), 0755); err != nil {
		t.Fatalf("create project %s: %v", path, err)
	}
	return path
}

// TestSessionPinnedProjectIgnoresRoots preserves the Claude Code contract: a
// server launched inside a project acts on that project, whatever else is
// lying around on disk.
func TestSessionPinnedProjectIgnoresRoots(t *testing.T) {
	root := t.TempDir()
	pinned := mkProj(t, filepath.Join(root, "pinned"))
	mkProj(t, filepath.Join(root, "other"))

	s := newSession(pinned, []string{root})
	got, err := s.projectRoot()
	if err != nil {
		t.Fatalf("projectRoot: %v", err)
	}
	if got != pinned {
		t.Errorf("acting on %q, want the pinned project %q", got, pinned)
	}
}

// TestSessionAutoSelectsSoleProject is the zero-friction path. Most users
// configure one project folder, and making them choose from a list of one
// would be friction with no purpose.
func TestSessionAutoSelectsSoleProject(t *testing.T) {
	root := t.TempDir()
	only := mkProj(t, filepath.Join(root, "only"))

	s := newSession("", []string{root})
	if s.current != only {
		t.Fatalf("current is %q, want the sole project auto-selected (%q)", s.current, only)
	}
}

// TestSessionAmbiguousProjectsGuideTheAgent covers the multi-project case. The
// error must name the way forward, not merely refuse.
func TestSessionAmbiguousProjectsGuideTheAgent(t *testing.T) {
	root := t.TempDir()
	mkProj(t, filepath.Join(root, "one"))
	mkProj(t, filepath.Join(root, "two"))

	s := newSession("", []string{root})
	if s.current != "" {
		t.Fatalf("auto-selected %q with two projects present; want no selection", s.current)
	}

	_, err := s.projectRoot()
	if err == nil {
		t.Fatal("projectRoot succeeded with an ambiguous selection")
	}
	if !strings.Contains(err.Error(), "avc_projects_list") {
		t.Errorf("error does not point at avc_projects_list: %v", err)
	}
}

// TestSessionUseByNameAndPath covers both ways an agent will refer to a
// project — the bare name a user says out loud, and the path from a listing.
func TestSessionUseByNameAndPath(t *testing.T) {
	root := t.TempDir()
	mkProj(t, filepath.Join(root, "one"))
	two := mkProj(t, filepath.Join(root, "two"))

	s := newSession("", []string{root})

	if _, err := s.use("two"); err != nil {
		t.Fatalf("use by name: %v", err)
	}
	if s.current != two {
		t.Errorf("current is %q, want %q", s.current, two)
	}

	if _, err := s.use(filepath.Join(root, "one")); err != nil {
		t.Fatalf("use by path: %v", err)
	}
	if s.current != filepath.Join(root, "one") {
		t.Errorf("current is %q, want the one project", s.current)
	}
}

// TestSessionUseRejectsUnconfiguredPath keeps the agent inside the folders the
// user chose. Accepting an arbitrary directory would let it act on projects
// the user never exposed.
func TestSessionUseRejectsUnconfiguredPath(t *testing.T) {
	root := t.TempDir()
	mkProj(t, filepath.Join(root, "inside"))
	outside := mkProj(t, filepath.Join(t.TempDir(), "outside"))

	s := newSession("", []string{root})
	if _, err := s.use(outside); err == nil {
		t.Error("use accepted a project outside the configured roots")
	}
}

// TestProjectlessToolsAreNotADeadEnd is the core UX guarantee. Before this
// change an unresolved project advertised zero tools, leaving the agent with
// no move to make and the user with no explanation.
func TestProjectlessToolsAreNotADeadEnd(t *testing.T) {
	withRoots := ProjectlessTools(true)
	if len(withRoots) == 0 {
		t.Fatal("no tools advertised without a project; the agent has no way forward")
	}
	want := map[string]bool{"avc_init": false, "avc_projects_list": false, "avc_project_use": false}
	for _, tool := range withRoots {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s is not advertised when no project is resolved", name)
		}
	}

	// Without search roots, listing and switching have nothing to operate on;
	// only initialization still makes sense.
	noRoots := ProjectlessTools(false)
	if len(noRoots) != 1 || noRoots[0].Name != "avc_init" {
		t.Errorf("without roots got %d tools, want only avc_init", len(noRoots))
	}
}

// TestAvcInitAdoptsTheNewProject covers the rescue path: a user points the
// extension at a folder that was never initialized, and one tool call makes it
// usable without them leaving the conversation for a terminal.
func TestAvcInitAdoptsTheNewProject(t *testing.T) {
	dir := t.TempDir()
	s := newSession("", []string{dir})

	_, handled, err := dispatchProjectTool(s, true, "avc_init", map[string]any{"path": dir})
	if !handled {
		t.Fatal("avc_init was not handled")
	}
	if err != nil {
		t.Fatalf("avc_init: %v", err)
	}
	if s.current != dir {
		t.Errorf("current is %q, want the freshly initialized %q", s.current, dir)
	}

	// Calling it again must not fail — the caller wanted a usable project and
	// already has one.
	if _, _, err := dispatchProjectTool(s, true, "avc_init", map[string]any{"path": dir}); err != nil {
		t.Errorf("avc_init on an existing project returned an error: %v", err)
	}
}

// TestDispatchProjectToolIgnoresOtherTools confirms the handled flag lets
// ordinary tools fall through to the project-scoped dispatcher.
func TestDispatchProjectToolIgnoresOtherTools(t *testing.T) {
	s := newSession("", nil)
	if _, handled, _ := dispatchProjectTool(s, true, "avc_snapshot", map[string]any{}); handled {
		t.Error("avc_snapshot was claimed by the project-tool dispatcher")
	}
}
