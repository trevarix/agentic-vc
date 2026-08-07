// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:   "delete <snapshot_id>",
	Short: "Delete a snapshot and its stored file objects",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false,
		"Delete even if the snapshot is protected (a branch base, tagged, or part of the last merge record)")
}

func runDelete(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Confirm the snapshot exists before attempting deletion.
	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	if !deleteForce {
		proj, err := store.GetProject(projectPath)
		if err != nil {
			return err
		}
		protected, err := store.IsSnapshotProtected(proj.ID, snapshotID)
		if err != nil {
			return err
		}
		if protected {
			return fmt.Errorf(
				"snapshot '%s' is protected (it is a branch base, tagged, or part of the last merge record); "+
					"pass --force to delete it anyway", snapshotID,
			)
		}
	}

	if err := store.DeleteSnapshot(snapshotID); err != nil {
		return fmt.Errorf("delete snapshot: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":      snapshotID,
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Deleted snapshot:"), dim(snapshotID))
	return nil
}
