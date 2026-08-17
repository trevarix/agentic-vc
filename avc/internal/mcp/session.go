// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/projects"
)

// session holds the per-connection state that decides which project a tool
// call acts on.
//
// Claude Code launches the server inside a project and `current` is fixed for
// the whole connection. Claude Desktop launches it with search roots instead
// and no working directory worth the name, so `current` starts empty and is
// resolved by discovery — automatically when there is exactly one project,
// and otherwise by the agent calling avc_project_use.
type session struct {
	roots   []string // configured search roots; empty for a project-bound server
	current string   // project root tools act on; empty until resolved
}

// newSession builds the connection state. A non-empty projectRoot pins the
// session to that project and ignores roots, preserving the CLI behaviour
// where the working directory is the project.
func newSession(projectRoot string, roots []string) *session {
	s := &session{roots: roots, current: projectRoot}
	if s.current == "" {
		s.autoSelect()
	}
	return s
}

// autoSelect picks the sole discovered project, if there is exactly one.
// Most users configure a single project directory, and asking them to choose
// from a list of one is friction with no purpose.
func (s *session) autoSelect() {
	if len(s.roots) == 0 {
		return
	}
	if found := projects.Discover(s.roots); len(found) == 1 {
		s.current = found[0].Path
	}
}

// projectRoot returns the project tools should act on, or an error naming the
// way forward. A tool must never fail with a bare "not an AVC project" when
// the user has projects configured but has not picked one.
func (s *session) projectRoot() (string, error) {
	if s.current != "" {
		return s.current, nil
	}
	if len(s.roots) == 0 {
		return "", fmt.Errorf("no AVC project found — run `avc init` in the project directory first")
	}

	found := projects.Discover(s.roots)
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no AVC projects found under %v — run `avc init` in a project directory, or call avc_init to set one up", s.roots)
	case 1:
		s.current = found[0].Path
		return s.current, nil
	default:
		return "", fmt.Errorf("%d AVC projects are available — call avc_projects_list to see them, then avc_project_use to choose one", len(found))
	}
}

// use switches the current project. The path must be a discovered project:
// accepting an arbitrary directory would let an agent operate outside the
// roots the user configured.
func (s *session) use(path string) (projects.Project, error) {
	for _, p := range projects.Discover(s.roots) {
		if p.Path == path || p.Name == path {
			s.current = p.Path
			return p, nil
		}
	}
	return projects.Project{}, fmt.Errorf("no configured AVC project matches %q — call avc_projects_list to see the available projects", path)
}

// wrapProject renders a project-tool result and tags it as handled, so the
// three handlers below can return in one line each.
func wrapProject(v any, compact bool) (map[string]any, bool, error) {
	res, err := wrapContent(v, compact)
	return res, true, err
}

// dispatchProjectTool handles the tools that resolve which project the session
// acts on. These run before a project exists, so they cannot go through
// dispatchTool, which requires one.
//
// handled reports whether name was a project tool at all, letting the caller
// fall through to the project-scoped tools without a second name lookup.
func dispatchProjectTool(sess *session, compact bool, name string, args map[string]any) (result map[string]any, handled bool, err error) {
	switch name {
	case "avc_projects_list":
		return wrapProject(map[string]any{
			"roots":    sess.roots,
			"projects": sess.list(),
		}, compact)

	case "avc_project_use":
		path, _ := args["project"].(string)
		if path == "" {
			return nil, true, fmt.Errorf("project is required — call avc_projects_list to see the available projects")
		}
		p, err := sess.use(path)
		if err != nil {
			return nil, true, err
		}
		return wrapProject(map[string]any{
			"name":    p.Name,
			"path":    p.Path,
			"current": true,
		}, compact)

	case "avc_init":
		path, _ := args["path"].(string)
		if path == "" {
			return nil, true, fmt.Errorf("path is required — give the absolute path of the directory to initialize")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, true, fmt.Errorf("resolve %q: %w", path, err)
		}
		if projects.IsProject(abs) {
			// Not an error: the caller wanted this directory usable, and it is.
			sess.current = abs
			return wrapProject(map[string]any{
				"path":            abs,
				"already_project": true,
				"current":         true,
			}, compact)
		}
		project, err := db.InitProject(abs)
		if err != nil {
			return nil, true, fmt.Errorf("initialize %s: %w", abs, err)
		}
		// A freshly initialized project is almost certainly the one the user
		// wants to work in, so adopt it rather than making them switch.
		sess.current = abs
		return wrapProject(map[string]any{
			"path":            abs,
			"project_id":      project.ID,
			"already_project": false,
			"current":         true,
		}, compact)
	}
	return nil, false, nil
}

// list returns the discovered projects and marks which one is current.
func (s *session) list() []map[string]any {
	found := projects.Discover(s.roots)
	out := make([]map[string]any, 0, len(found))
	for _, p := range found {
		out = append(out, map[string]any{
			"name":    p.Name,
			"path":    p.Path,
			"current": p.Path == s.current,
		})
	}
	return out
}
