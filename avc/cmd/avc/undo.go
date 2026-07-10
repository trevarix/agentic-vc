// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/undo"
	"github.com/spf13/cobra"
)

var undoList bool

var undoCmd = &cobra.Command{
	Use:   "undo",
	Short: "Reverse the most recent restore or merge",
	Long: `Reverses the newest entry in the operations log — the safety snapshot AVC
took before the last restore or merge is restored, with zero arguments.

Undoing a merge also reactivates the merged branch and rebuilds its workspace.
Running undo twice acts as redo: every undo records itself into the same log.

Use --list to see recent operations and what undo would reverse.`,
	RunE: runUndo,
}

func init() {
	undoCmd.Flags().BoolVar(&undoList, "list", false, "List recent operations instead of undoing")
	rootCmd.AddCommand(undoCmd)
}

func runUndo(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if undoList {
		return runUndoList(projectPath)
	}

	result, err := undo.Undo(projectPath)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"undone_kind":          result.UndoneKind,
			"undone_details":       result.UndoneDetails,
			"restored_snapshot_id": result.RestoredSnapshotID,
			"redo_snapshot_id":     result.RedoSnapshotID,
			"branch":               result.BranchName,
			"reactivated_branch":   result.ReactivatedBranch,
			"success":              true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Undid:"), bold(result.UndoneDetails))
	fmt.Printf("  %s %s %s\n", prop("Restored:"), cyan(result.RestoredSnapshotID), dim("on "+result.BranchName))
	if result.ReactivatedBranch != "" {
		fmt.Printf("  %s %s\n", prop("Branch:  "), green(result.ReactivatedBranch+" is active again (workspace rebuilt)"))
	}
	fmt.Printf("  %s %s\n", prop("Redo:    "), dim("run `avc undo` again"))
	return nil
}

func runUndoList(projectPath string) error {
	ops, err := undo.List(projectPath, 20)
	if err != nil {
		return err
	}

	if jsonOutput {
		type opJSON struct {
			ID             string `json:"id"`
			Kind           string `json:"kind"`
			Details        string `json:"details"`
			UndoSnapshotID string `json:"undo_snapshot_id"`
			BranchID       string `json:"branch_id"`
			CreatedAt      int64  `json:"created_at"`
		}
		out := make([]opJSON, len(ops))
		for i, op := range ops {
			out[i] = opJSON{op.ID, op.Kind, op.Details, op.UndoSnapshotID, op.BranchID, op.CreatedAt}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(ops) == 0 {
		fmt.Printf("%s\n", dim("No operations recorded yet — nothing to undo."))
		return nil
	}

	fmt.Printf("%s\n\n", accent("◆ Recent operations (newest first — `avc undo` reverses the top one):"))
	for i, op := range ops {
		marker := " "
		if i == 0 {
			marker = success("→")
		}
		fmt.Printf(" %s %s  %-8s %s  %s\n",
			marker,
			dim(time.Unix(op.CreatedAt, 0).Format("2006-01-02 15:04:05")),
			yellow(op.Kind), op.Details,
			dim("undo → "+op.UndoSnapshotID),
		)
	}
	return nil
}
