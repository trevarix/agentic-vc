package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [project_path]",
	Short: "Initialize AVC for a project",
	Long: `Creates a .avc/ directory inside the project with a SQLite database,
default config, and .gitignore. Defaults to the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	absPath, err := resolveProjectPath(projectPath)
	if err != nil {
		return err
	}

	project, err := db.InitProject(absPath)
	if err != nil {
		return fmt.Errorf("failed to initialize project: %w", err)
	}

	if err := config.WriteDefault(absPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":      project.ID,
			"path":    project.Path,
			"name":    project.Name,
			"success": true,
		})
	}

	fmt.Printf("Initialized AVC project at %s\n", absPath)
	fmt.Printf("Project ID: %s\n", project.ID)
	return nil
}
