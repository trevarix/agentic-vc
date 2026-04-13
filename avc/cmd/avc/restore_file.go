package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/spf13/cobra"
)

var restoreFileCmd = &cobra.Command{
	Use:   "restore-file <snapshot_id> <file_path>",
	Short: "Restore a single file from a snapshot",
	Args:  cobra.ExactArgs(2),
	RunE:  runRestoreFile,
}

func init() {
	rootCmd.AddCommand(restoreFileCmd)
}

func runRestoreFile(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]
	filePath := args[1]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	result, err := restore.RestoreFile(projectPath, snapshotID, filePath)
	if err != nil {
		return fmt.Errorf("restore-file failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":        result.SnapshotID,
			"file_path": result.FilePath,
			"size":      result.Size,
			"success":   true,
			"message":   fmt.Sprintf("Restored %s from snapshot %s", result.FilePath, result.SnapshotID),
		})
	}

	fmt.Printf("Restored %s from snapshot %s (%d bytes)\n", result.FilePath, result.SnapshotID, result.Size)
	return nil
}
