package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/skills"
	"github.com/spf13/cobra"
)

var initSkills []string

var initCmd = &cobra.Command{
	Use:   "init [project_path]",
	Short: "Initialize AVC for a project",
	Long: `Creates a .avc/ directory inside the project with a SQLite database,
default config, and .gitignore. Defaults to the current directory.
Safe to re-run on an already-initialized project.

Use --skills to wire up AVC as an MCP server for your agent framework.
Accepts a comma-separated list of frameworks:

  avc init --skills claude-code
  avc init --skills claude-code,cursor
  avc init --skills claude-code,cursor,windsurf,generic

Supported frameworks: claude-code, cursor, windsurf, generic`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringSliceVar(&initSkills, "skills", nil,
		"Comma-separated list of agent frameworks to configure (claude-code, cursor, windsurf, generic)")
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

	// Ensure main branch exists and backfill any pre-Phase-4 snapshots.
	store, err := db.Open(absPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if _, err := store.EnsureMainBranch(project.ID); err != nil {
		store.Close()
		return fmt.Errorf("failed to create main branch: %w", err)
	}
	store.Close()

	if err := config.WriteDefault(absPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Write framework-specific MCP configs and agent instruction files.
	var skillResults []*skills.WriteResult
	for _, framework := range initSkills {
		result, err := skills.Write(absPath, framework)
		if err != nil {
			return fmt.Errorf("--skills %s: %w", framework, err)
		}
		skillResults = append(skillResults, result)
	}

	if jsonOutput {
		type actionJSON struct {
			Path   string `json:"path"`
			Status string `json:"status"`
			Reason string `json:"reason,omitempty"`
		}
		type skillJSON struct {
			Framework string       `json:"framework"`
			Actions   []actionJSON `json:"actions"`
			Warnings  []string     `json:"warnings,omitempty"`
		}
		srJSON := make([]skillJSON, len(skillResults))
		for i, sr := range skillResults {
			actions := make([]actionJSON, len(sr.Actions))
			for j, a := range sr.Actions {
				actions[j] = actionJSON{a.Path, a.Status, a.Reason}
			}
			srJSON[i] = skillJSON{sr.Framework, actions, sr.Warnings}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":      project.ID,
			"path":    project.Path,
			"name":    project.Name,
			"skills":  srJSON,
			"success": true,
		})
	}

	fmt.Printf("Initialized AVC project at %s\n", absPath)
	fmt.Printf("Project ID: %s\n", project.ID)
	for _, sr := range skillResults {
		fmt.Printf("\nSkills for %s:\n", sr.Framework)
		for _, w := range sr.Warnings {
			fmt.Printf("  Warning: %s\n", w)
		}
		for _, a := range sr.Actions {
			switch a.Status {
			case "created":
				fmt.Printf("  created  %s\n", a.Path)
			case "updated":
				fmt.Printf("  updated  %s\n", a.Path)
			case "skipped":
				fmt.Printf("  skipped  %s  (%s)\n", a.Path, a.Reason)
			}
		}
	}
	if len(skillResults) > 0 {
		fmt.Println("\nStart the MCP server with: avc mcp serve")
	}
	return nil
}
