// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/spf13/cobra"
)

var (
	diffStatMode bool
	diffNoCache  bool
)

var diffCmd = &cobra.Command{
	Use:   "diff <from_snapshot_id> <to_snapshot_id>",
	Short: "Show file-by-file changes between two snapshots",
	Long: `Compares two snapshots and shows what changed.

Use --stat for a compact summary (file names + line counts only, no diff text).
Use --no-cache to bypass the diff cache and recompute from scratch.
Use --json for machine-readable output.`,
	Args: cobra.ExactArgs(2),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().BoolVar(&diffStatMode, "stat", false, "Show compact summary (file names + line counts) instead of full diff")
	diffCmd.Flags().BoolVar(&diffNoCache, "no-cache", false, "Bypass the diff cache and recompute from scratch")
}

func runDiff(cmd *cobra.Command, args []string) error {
	fromID := args[0]
	toID := args[1]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	result, err := diff.Compare(projectPath, fromID, toID)
	if err != nil {
		return fmt.Errorf("diff failed: %w", err)
	}

	if jsonOutput {
		type fileDiffJSON struct {
			Path         string `json:"path"`
			Type         string `json:"type"`
			OldHash      string `json:"old_hash,omitempty"`
			NewHash      string `json:"new_hash,omitempty"`
			LinesAdded   int    `json:"lines_added"`
			LinesRemoved int    `json:"lines_removed"`
			DiffPreview  string `json:"diff_preview,omitempty"`
		}
		files := make([]fileDiffJSON, len(result.Files))
		for i, f := range result.Files {
			files[i] = fileDiffJSON{
				Path:         f.Path,
				Type:         string(f.Type),
				OldHash:      f.OldHash,
				NewHash:      f.NewHash,
				LinesAdded:   f.LinesAdded,
				LinesRemoved: f.LinesRemoved,
				DiffPreview:  f.DiffPreview,
			}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"from_snapshot": fromID,
			"to_snapshot":   toID,
			"files":         files,
		})
	}

	if diffStatMode {
		printDiffStat(result.Files)
		return nil
	}

	fmt.Printf("%s %s %s %s\n%s\n\n", accent("◆ Diff:"), dim(fromID), dim("→"), dim(toID), ruler(50))
	for _, f := range result.Files {
		symbol, pathColor := changeSymbol(string(f.Type))
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf("%s %s (%s %s)\n", symbol, pathColor(f.Path), added, removed)
	}
	return nil
}

// printDiffStat prints a compact git-style stat summary:
//
//	src/auth.go      | +15  -3
//	src/users.go     | +42  -8
//	──────────────────────────
//	2 files changed  +57 -11
func printDiffStat(files []*diff.FileDiff) {
	if len(files) == 0 {
		fmt.Println("No changes.")
		return
	}

	// Find the longest path for alignment.
	maxLen := 0
	for _, f := range files {
		if len(f.Path) > maxLen {
			maxLen = len(f.Path)
		}
	}

	totalAdded, totalRemoved := 0, 0
	for _, f := range files {
		pad := strings.Repeat(" ", maxLen-len(f.Path))
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf(" %s%s  %s  %s  %s\n", f.Path, pad, dim("|"), added, removed)
		totalAdded += f.LinesAdded
		totalRemoved += f.LinesRemoved
	}

	fmt.Printf(" %s\n", dim(strings.Repeat("─", maxLen+14)))
	fileWord := "file"
	if len(files) != 1 {
		fileWord = "files"
	}
	fmt.Printf(" %s  %s  %s\n",
		accent(fmt.Sprintf("%d %s changed", len(files), fileWord)),
		green(fmt.Sprintf("+%d", totalAdded)),
		red(fmt.Sprintf("-%d", totalRemoved)),
	)
}

func changeSymbol(changeType string) (symbol string, pathColor func(string) string) {
	switch changeType {
	case "added":
		return green("A"), green
	case "deleted":
		return red("D"), red
	default:
		return yellow("M"), yellow
	}
}
