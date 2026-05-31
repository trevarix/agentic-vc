package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/spf13/cobra"
)

var fileHistoryCmd = &cobra.Command{
	Use:   "file-history <file_path>",
	Short: "List snapshots containing a specific file",
	Args:  cobra.ExactArgs(1),
	RunE:  runFileHistory,
}

func init() {
	rootCmd.AddCommand(fileHistoryCmd)
}

func runFileHistory(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	history, err := snapshot.FileHistory(projectPath, filePath)
	if err != nil {
		return fmt.Errorf("file-history failed: %w", err)
	}

	if jsonOutput {
		if history == nil {
			history = []*snapshot.FileHistoryEntry{}
		}
		return json.NewEncoder(os.Stdout).Encode(history)
	}

	if len(history) == 0 {
		fmt.Printf("File '%s' not found in any snapshot.\n", filePath)
		return nil
	}

	fmt.Printf("%s — found in %d snapshot(s)\n\n", bold(filePath), len(history))
	for _, e := range history {
		ts := time.Unix(e.Timestamp, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%s  %s  %s  %s\n",
			cyan(e.SnapshotID),
			dim(ts),
			yellow(e.Label),
			dim(e.Hash[:8]),
		)
	}
	return nil
}
