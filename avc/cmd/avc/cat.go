package avc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
	"github.com/spf13/cobra"
)

var catCmd = &cobra.Command{
	Use:   "cat <snapshot_id> <file_path>",
	Short: "Print the contents of a file from a snapshot to stdout",
	Args:  cobra.ExactArgs(2),
	RunE:  runCat,
}

func init() {
	rootCmd.AddCommand(catCmd)
}

func runCat(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]
	filePath := args[1]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	data, err := snapshot.CatFile(projectPath, snapshotID, filePath)
	if err != nil {
		return fmt.Errorf("cat failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"snapshot_id":    snapshotID,
			"file_path":      filePath,
			"size":           len(data),
			"content_base64": base64.StdEncoding.EncodeToString(data),
		})
	}

	// Binary-safe raw write to stdout (Unix-cat behaviour).
	_, err = os.Stdout.Write(data)
	return err
}
