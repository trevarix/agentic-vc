// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package projects discovers AVC projects on disk beneath one or more search
// roots.
//
// A Claude Code server binds to a single project because the working directory
// is the project. Claude Desktop has no meaningful working directory, and a
// Desktop user is reviewing and recovering across every project they own, so
// that server is configured with search roots instead and discovers the
// projects underneath them.
package projects

import (
	"os"
	"path/filepath"
	"sort"
)

// maxDepth bounds how far below a search root a project may sit. Users point
// AVC at a directory of repositories, not at their home directory, so a small
// bound keeps discovery fast on large trees while still finding projects
// nested a couple of levels down (e.g. ~/code/work/api).
const maxDepth = 4

// avcDir is the marker directory that makes a path an AVC project.
const avcDir = ".avc"

// skipDirs are never descended into. They are large, never contain a project
// the user means to manage, and dominate walk time when they are present.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"__pycache__":  true,
	"venv":         true,
	".venv":        true,
}

// Project is a discovered AVC project.
type Project struct {
	Name string `json:"name"` // base name of the project directory
	Path string `json:"path"` // absolute path to the project root (the .avc parent)
}

// Discover returns every AVC project beneath the given roots, sorted by name
// then path so output is stable across calls.
//
// Unreadable directories are skipped rather than failing the whole walk: a
// single permission-denied subdirectory must not hide every other project.
// A root that is itself a project is returned without descending into it,
// since AVC workspaces under .avc/workspaces/ would otherwise appear as
// projects in their own right.
func Discover(roots []string) []Project {
	seen := map[string]bool{}
	var found []Project

	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		walk(abs, 0, seen, &found)
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].Name != found[j].Name {
			return found[i].Name < found[j].Name
		}
		return found[i].Path < found[j].Path
	})
	return found
}

// walk visits dir, recording it when it is a project and otherwise recursing
// into its subdirectories until maxDepth.
func walk(dir string, depth int, seen map[string]bool, found *[]Project) {
	if depth > maxDepth {
		return
	}

	if isProject(dir) {
		if !seen[dir] {
			seen[dir] = true
			*found = append(*found, Project{Name: filepath.Base(dir), Path: dir})
		}
		// A project's own internals never contain other projects worth
		// listing — .avc/workspaces/<branch> is a copy of this same project.
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if skipDirs[name] || (len(name) > 0 && name[0] == '.') {
			continue
		}
		walk(filepath.Join(dir, name), depth+1, seen, found)
	}
}

// IsProject reports whether dir is an AVC project root.
func IsProject(dir string) bool { return isProject(dir) }

func isProject(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, avcDir))
	return err == nil && info.IsDir()
}
