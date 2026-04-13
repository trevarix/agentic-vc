package avc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
)

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

	cfg, _ := config.Load(projectRoot)
	if cfg.Branch.Active == "" {
		return config.SetActiveBranch(projectRoot, "main")
	}
	return nil
}
