// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
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

	// Blame-style: one row per contiguous block of lines from the same snapshot,
	// rather than repeating the label on every line.
	fmt.Printf("%s  %s\n\n", accent(result.FilePath), dim(fmt.Sprintf("(%d lines)", result.TotalLines)))
	for _, b := range annotate.CollapseBlocks(result.Lines) {
		rangeStr := fmt.Sprintf("%d", b.Start)
		if b.End != b.Start {
			rangeStr = fmt.Sprintf("%d-%d", b.Start, b.End)
		}
		lineCol := dim(fmt.Sprintf("%9s", rangeStr))

		// Untracked lines have no snapshot to attribute.
		if b.Line.SnapshotID == "" {
			fmt.Printf("%s %s %s\n", lineCol, dim("│"), dim("(untracked)"))
			continue
		}

		lbl := b.Line.Label
		if utf8.RuneCountInString(lbl) > 28 {
			lbl = string([]rune(lbl)[:25]) + "..."
		}
		author, isAgent := annotate.ClassifyAuthor(b.Line.AgentName)
		authorCol := dim(fmt.Sprintf("%-8s", author))
		if isAgent {
			authorCol = blue(fmt.Sprintf("%-8s", author))
		}
		fmt.Printf("%s %s %s  %s  %s\n",
			lineCol,
			dim("│"),
			cyan(fmt.Sprintf("%-28s", lbl)),
			authorCol,
			dim(relativeTime(b.Line.Timestamp)),
		)
	}
	return nil
}

// relativeTime renders a Unix timestamp as a short "3d ago" style string.
// Returns "" for a zero timestamp (no known time).
func relativeTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	}
}
