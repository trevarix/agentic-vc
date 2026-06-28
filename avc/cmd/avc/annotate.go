// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/annotate"
	"github.com/spf13/cobra"
)

var annotateCmd = &cobra.Command{
	Use:   "annotate <file_path>",
	Short: "Show which snapshot introduced each line of a file",
	Args:  cobra.ExactArgs(1),
	RunE:  runAnnotate,
}

func init() {
	rootCmd.AddCommand(annotateCmd)
}

func runAnnotate(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	result, err := annotate.Annotate(projectPath, filePath)
	if err != nil {
		return fmt.Errorf("annotate failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	fmt.Printf("%s (%d lines)\n\n", bold(result.FilePath), result.TotalLines)
	for _, line := range result.Lines {
		label := line.Label
		if len(label) > 24 {
			label = label[:21] + "..."
		}
		agent := ""
		if line.AgentName != "" {
			agent = fmt.Sprintf(" (%s)", line.AgentName)
		}
		fmt.Printf("%4d │ %-24s%s\n", line.Line, label, agent)
	}
	return nil
}
