// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/trevarix/agentic-vc/avc/internal/skills"
)

var doctorAgents bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that agent integrations are set up and consistent",
	Long: `Inspects the agent tools set up in this project with avc init --skills and
reports, for each one, whether:

  mcp server     its MCP config has an avc entry whose command runs
  rules          its rules file exists and matches the configured tool tier
  sharing        its files are trackable, or gitignored (local only)
  pre-edit hook  the session checkpoint hook is installed

Exits 1 when any check fails, so it can gate CI or a setup script.`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorAgents, "agents", true,
		"Check agent tool integrations (currently the only check)")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	projectRoot, err := requireInitializedProject()
	if err != nil {
		return err
	}
	var reports []skills.AgentReport
	if doctorAgents {
		reports = skills.Diagnose(projectRoot)
	}
	failed := anyFailed(reports)

	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"project": projectRoot,
			"agents":  reports,
			"ok":      !failed,
		}); err != nil {
			return err
		}
	} else {
		printDoctor(reports)
	}
	if failed {
		return fmt.Errorf("one or more agent checks failed")
	}
	return nil
}

func anyFailed(reports []skills.AgentReport) bool {
	for _, r := range reports {
		for _, c := range r.Checks {
			if c.Status == skills.StatusFail {
				return true
			}
		}
	}
	return false
}

func printDoctor(reports []skills.AgentReport) {
	if len(reports) == 0 {
		fmt.Println(dim("No agent tools are set up here. Run avc init --skills <tool> — supported: claude-code, cursor, windsurf, codex, copilot, generic."))
		return
	}
	for _, r := range reports {
		fmt.Printf("%s %s\n", accent("◆"), bold(r.Framework))
		for _, c := range r.Checks {
			fmt.Printf("  %s %-14s %s\n", statusSymbol(c.Status), c.Name, c.Detail)
		}
		fmt.Println()
	}
}

func statusSymbol(status string) string {
	switch status {
	case skills.StatusOK:
		return success("✓")
	case skills.StatusWarn:
		return warn("⚠")
	case skills.StatusFail:
		return failure("✗")
	default:
		return dim("·")
	}
}
