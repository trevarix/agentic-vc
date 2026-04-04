package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/agentic-version-ctl/avc/internal/diff"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <from_snapshot_id> <to_snapshot_id>",
	Short: "Show file-by-file changes between two snapshots",
	Args:  cobra.ExactArgs(2),
	RunE:  runDiff,
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

	fmt.Printf("%s %s → %s\n\n", bold("Diff:"), dim(fromID), dim(toID))
	for _, f := range result.Files {
		symbol, pathColor := changeSymbol(string(f.Type))
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf("%s %s (%s %s)\n", symbol, pathColor(f.Path), added, removed)
	}
	return nil
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
