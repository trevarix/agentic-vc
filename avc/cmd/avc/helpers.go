// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// isAVCDir reports whether path already contains an .avc directory.
func isAVCDir(path string) bool {
	fi, err := os.Stat(filepath.Join(path, ".avc"))
	return err == nil && fi.IsDir()
}

// resolveProjectPath converts a potentially relative path to an absolute one.
func resolveProjectPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid project path: %w", err)
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		return "", fmt.Errorf("project path does not exist: %s", abs)
	}
	return abs, nil
}

// requireInitializedProject finds the .avc directory starting from the current
// working directory and walking up the tree. Returns the project root path.
// Also ensures the main branch exists and config has an active branch set
// (handles transparent Phase 1 → Phase 4 migration).
func requireInitializedProject() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		avcDir := filepath.Join(dir, ".avc")
		if info, err := os.Stat(avcDir); err == nil && info.IsDir() {
			if err := ensureMainBranchSetup(dir); err != nil {
				return "", err
			}
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("no AVC project found (run `avc init` first)")
}

// ensureMainBranchSetup creates the main branch for the project if it doesn't
// exist yet and ensures the config has an active branch set. This is the
// migration path for Phase 1 projects being opened with Phase 4 code.
func ensureMainBranchSetup(projectRoot string) error {
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		// Project not yet initialized — nothing to migrate.
		return nil
	}

	if _, err := store.EnsureMainBranch(project.ID); err != nil {
		return fmt.Errorf("branch setup: %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		// A malformed config.toml must be a loud, actionable error — not a
		// nil-pointer panic, and not silently-dropped ignore/protect rules.
		return fmt.Errorf(".avc/config.toml is malformed: %w", err)
	}
	if cfg.Branch.Active == "" {
		return config.SetActiveBranch(projectRoot, "main")
	}
	return nil
}
