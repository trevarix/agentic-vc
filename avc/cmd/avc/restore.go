// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot_id>",
	Short: "Restore the project to a previous snapshot",
	Long: `Overwrites current project files with the contents from the specified snapshot.
The snapshot must belong to the active branch.
This is a destructive operation — save a snapshot of the current state first if needed.`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

func runRestore(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	// Branch scoping: verify the snapshot belongs to the active branch.
	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	snap, snapErr := store.GetSnapshot(snapshotID)
	store.Close()

	if snapErr != nil {
		return fmt.Errorf("snapshot '%s' not found", snapshotID)
	}
	if snap.BranchID != "" {
		activeBranchID, err := branchpkg.GetActiveBranchID(projectPath)
		if err == nil && snap.BranchID != activeBranchID {
			return fmt.Errorf(
				"snapshot '%s' belongs to a different branch\n"+
					"Switch branches first: avc branch switch <name>",
				snapshotID,
			)
		}
	}

	// For non-main branches, restore into the workspace rather than the real
	// project root so that main is not touched.
	targetDir := projectPath
	branchName := branchpkg.GetActiveBranchName(projectPath)
	if ws := branchpkg.WorkspacePath(projectPath, branchName); ws != "" {
		targetDir = ws
	}

	result, err := restore.RestoreToDir(projectPath, snapshotID, targetDir)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":                result.SnapshotID,
			"restored_files":    result.RestoredFiles,
			"restored_size":     result.RestoredSize,
			"quarantined_files": result.QuarantinedFiles,
			"trash_op_id":       result.TrashOpID,
			"success":           true,
			"message":           fmt.Sprintf("Successfully restored snapshot %s", result.SnapshotID),
		})
	}

	fmt.Printf("%s %s\n", success("✓ Restored:"), cyan(result.SnapshotID))
	fmt.Printf("  %s %s\n", prop("Files restored:"), green(fmt.Sprintf("%d", result.RestoredFiles)))
	fmt.Printf("  %s %s\n", prop("Total size:    "), dim(fmt.Sprintf("%d bytes", result.RestoredSize)))
	if result.QuarantinedFiles > 0 {
		fmt.Printf("  %s %s\n", prop("Quarantined:   "),
			yellow(fmt.Sprintf("%d untracked file(s) moved to trash (avc trash list)", result.QuarantinedFiles)))
	}
	return nil
}
