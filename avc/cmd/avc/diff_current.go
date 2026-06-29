// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/spf13/cobra"
)

var diffCurrentCmd = &cobra.Command{
	Use:   "diff-current <snapshot_id>",
	Short: "Show changes between a snapshot and the current working tree",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiffCurrent,
}

func init() {
	rootCmd.AddCommand(diffCurrentCmd)
}

func runDiffCurrent(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	result, err := diff.CompareWithCurrent(projectPath, snapshotID)
	if err != nil {
		return fmt.Errorf("diff-current failed: %w", err)
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
			"from_snapshot": snapshotID,
			"to_snapshot":   "working-tree",
			"files":         files,
		})
	}

	fmt.Printf("%s %s → %s\n\n", bold("Diff:"), dim(snapshotID), dim("working tree"))
	for _, f := range result.Files {
		symbol, pathColor := changeSymbol(string(f.Type))
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf("%s %s (%s %s)\n", symbol, pathColor(f.Path), added, removed)
	}
	return nil
}
