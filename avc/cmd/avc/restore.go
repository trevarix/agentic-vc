package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/spf13/cobra"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot_id>",
	Short: "Restore the project to a previous snapshot",
	Long: `Overwrites current project files with the contents from the specified snapshot.
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

	result, err := restore.Restore(projectPath, snapshotID)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":             result.SnapshotID,
			"restored_files": result.RestoredFiles,
			"restored_size":  result.RestoredSize,
			"success":        true,
			"message":        fmt.Sprintf("Successfully restored snapshot %s", result.SnapshotID),
		})
	}

	fmt.Printf("%s %s\n", bold("Restored:"), cyan(result.SnapshotID))
	fmt.Printf("  Files restored: %s\n", green(fmt.Sprintf("%d", result.RestoredFiles)))
	fmt.Printf("  Total size:     %s\n", dim(fmt.Sprintf("%d bytes", result.RestoredSize)))
	return nil
}
