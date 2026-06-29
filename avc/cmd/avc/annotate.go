// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/trevarix/agentic-vc/avc/internal/annotate"
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

	fmt.Printf("%s  %s\n\n", accent(result.FilePath), dim(fmt.Sprintf("(%d lines)", result.TotalLines)))
	for _, line := range result.Lines {
		lbl := line.Label
		if utf8.RuneCountInString(lbl) > 24 {
			lbl = string([]rune(lbl)[:21]) + "..."
		}
		agent := ""
		if line.AgentName != "" {
			agent = "  " + blue(line.AgentName)
		}
		labelColor := cyan
		if lbl == "(untracked)" {
			labelColor = dim
		}
		fmt.Printf("%s %s %s%s\n",
			dim(fmt.Sprintf("%4d", line.Line)),
			dim("│"),
			labelColor(fmt.Sprintf("%-24s", lbl)),
			agent,
		)
	}
	return nil
}
