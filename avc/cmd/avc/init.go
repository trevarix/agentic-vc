// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/skills"
	"github.com/spf13/cobra"
)

var (
	initSkills       []string
	initYes          bool
	initSkillsGlobal bool
)

var initCmd = &cobra.Command{
	Use:   "init [project_path]",
	Short: "Initialize AVC for a project",
	Long: `Creates a .avc/ directory inside the project with a SQLite database,
default config, and .avcignore. Defaults to the current directory.
Creates the directory if it does not exist. Safe to re-run — existing
snapshots, branches, and config are left untouched.

If no AVC project exists at the path yet, you'll be asked to confirm before
one is created. Pass --yes to skip the prompt (e.g. in scripts or CI); --json
mode also skips it, since machine consumers are expected to know what they
asked for.

Use --skills to wire up AVC as an MCP server for your agent framework.
Accepts a comma-separated list of frameworks:

  avc init --skills claude-code
  avc init --skills claude-desktop
  avc init --skills claude-code,cursor
  avc init --skills claude-code,cursor,windsurf,generic

MCP configs are written at the project level where the framework supports it
(claude-code: .mcp.json, cursor: .cursor/mcp.json), so the server is scoped to
this project and the config can be committed. Pass --global to write the
framework's global (home-directory) config instead. claude-desktop and
windsurf only support global configs and always write there.

Supported frameworks: claude-code, claude-desktop, cursor, windsurf, generic`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringSliceVar(&initSkills, "skills", nil,
		"Comma-separated list of agent frameworks to configure (claude-code, claude-desktop, cursor, windsurf, generic)")
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false,
		"Skip the confirmation prompt when no AVC project exists at the path yet")
	initCmd.Flags().BoolVar(&initSkillsGlobal, "global", false,
		"Write MCP configs to the framework's global config instead of the project level")
}

// confirmNewProject asks the user whether to create a new AVC project at path.
// Declines (returns false) on any input error — e.g. no TTY attached — so an
// unattended run never bootstraps a project the caller didn't explicitly want.
func confirmNewProject(path string) bool {
	fmt.Printf("%s %s\n", warn("⚠ No AVC project found at"), cyan(path))
	fmt.Print("  Initialize a new AVC project here? [y/N] ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func runInit(cmd *cobra.Command, args []string) error {
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("invalid project path: %w", err)
	}

	alreadyInit := isAVCDir(absPath)

	// Bootstrapping a brand-new project is consequential — it creates a
	// directory, a database, and (with --skills) writes agent configs.
	// Confirm with the user unless they've opted out via --yes or --json
	// (machine consumers are presumed to know what they're asking for).
	if !alreadyInit && !initYes && !jsonOutput {
		if !confirmNewProject(absPath) {
			fmt.Println(dim("Aborted — no changes made."))
			return nil
		}
	}

	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return fmt.Errorf("could not create project directory: %w", err)
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
		result, err := skills.Write(absPath, framework, initSkillsGlobal)
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
			"id":                 project.ID,
			"path":               project.Path,
			"name":               project.Name,
			"skills":             srJSON,
			"already_initialized": alreadyInit,
			"success":            true,
		})
	}

	if alreadyInit {
		fmt.Printf("%s %s\n", accent("◆ AVC already initialized at"), cyan(absPath))
		fmt.Printf("  %s %s\n", prop("Project ID:"), dim(project.ID))
	} else {
		fmt.Printf("%s %s\n", success("✓ Initialized AVC project at"), cyan(absPath))
		fmt.Printf("  %s %s\n", prop("Project ID:"), dim(project.ID))
	}
	for _, sr := range skillResults {
		fmt.Printf("\n%s %s\n", accent("◆ Skills:"), bold(sr.Framework))
		for _, w := range sr.Warnings {
			fmt.Printf("  %s %s\n", warn("⚠"), w)
		}
		for _, a := range sr.Actions {
			switch a.Status {
			case "created":
				fmt.Printf("  %s  %s\n", success("✓ created"), cyan(a.Path))
			case "updated":
				fmt.Printf("  %s  %s\n", warn("↑ updated"), yellow(a.Path))
			case "skipped":
				fmt.Printf("  %s  %s  %s\n", dim("  skipped"), dim(a.Path), dim("("+a.Reason+")"))
			}
		}
	}
	if len(skillResults) > 0 {
		fmt.Printf("\n%s\n", dim("Start the MCP server with: avc mcp serve"))
	}
	return nil
}
