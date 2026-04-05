package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show snapshot history as a tree diagram",
	RunE:  runLog,
}

func init() {
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	snapshots, err := store.ListSnapshots()
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(snapshots)
	}

	if len(snapshots) == 0 {
		fmt.Println("No snapshots yet. Run `avc snapshot <label>` to create one.")
		return nil
	}

	fmt.Println(bold("  Snapshot history"))
	fmt.Println()

	for i, s := range snapshots {
		ts := time.Unix(s.Timestamp, 0).Format("2006-01-02 15:04:05")

		// Node
		fmt.Printf("  %s  %s  %s\n",
			cyan("●"),
			cyan(s.ID),
			bold(s.Label),
		)

		// Metadata row
		agent := dim("—")
		if s.AgentName != "" {
			agent = dim(s.AgentName)
		}
		notes := ""
		if s.Notes != "" {
			notes = "  " + dim("\""+s.Notes+"\"")
		}
		fmt.Printf("  %s  %s  agent: %s  files: %s%s\n",
			dim("│"),
			dim(ts),
			agent,
			yellow(fmt.Sprintf("%d", s.FileCount)),
			notes,
		)

		// Connector or base
		if i < len(snapshots)-1 {
			fmt.Printf("  %s\n", dim("│"))
		} else {
			fmt.Printf("  %s\n", dim("◎  (root)"))
		}
	}

	fmt.Println()
	return nil
}
