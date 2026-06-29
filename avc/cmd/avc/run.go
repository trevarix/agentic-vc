// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	workspacepkg "github.com/trevarix/agentic-vc/avc/internal/workspace"
)

var runBranch string
var runTimeout int

var runCmd = &cobra.Command{
	Use:   "run --branch <name> <command...>",
	Short: "Run a command in an agent branch workspace",
	Long: `Execute a shell command inside the materialized workspace for a branch.
The command runs with environment scrubbing, an execution timeout, and process
tree kill on timeout. Use this to run tests or builds against workspace files.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRunCmd(args)
	},
}

func init() {
	runCmd.Flags().StringVar(&runBranch, "branch", "", "Branch whose workspace to run in (required)")
	runCmd.Flags().IntVar(&runTimeout, "timeout", 0, "Timeout in seconds (default from config)")
	runCmd.MarkFlagRequired("branch")
}

func runRunCmd(args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	command := strings.Join(args, " ")

	result, err := workspacepkg.Run(workspacepkg.RunRequest{
		ProjectRoot:    projectPath,
		BranchName:     runBranch,
		Command:        command,
		TimeoutSeconds: runTimeout,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"exit_code":      result.ExitCode,
			"stdout":         result.Stdout,
			"stderr":         result.Stderr,
			"workspace_path": result.WorkspacePath,
			"env_info": map[string]any{
				"type": result.EnvInfo.Type,
				"path": result.EnvInfo.Path,
			},
			"sandbox_info": map[string]any{
				"platform": result.SandboxInfo.Platform,
				"layers": map[string]any{
					"env_scrubbing":     result.SandboxInfo.EnvScrubbing,
					"execution_limits":  result.SandboxInfo.ExecutionLimits,
					"process_tree_kill": result.SandboxInfo.ProcessTreeKill,
				},
			},
		})
	}

	if result.Stdout != "" {
		fmt.Print(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
	}

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}
	return nil
}
