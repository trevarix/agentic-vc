package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	branchpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List snapshots for the active branch",
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	cfg, _ := config.Load(projectPath)
	activeName := cfg.Branch.Active
	if activeName == "" {
		activeName = "main"
	}

	// Attempt to filter by active branch; fall back to all snapshots if
	// the branch record doesn't exist yet (e.g. very early init edge case).
	var snapshots []*db.Snapshot
	branchID, branchErr := branchpkg.GetActiveBranchID(projectPath)
	if branchErr == nil {
		snapshots, err = store.ListSnapshotsByBranch(branchID)
	} else {
		snapshots, err = store.ListSnapshots()
	}
	if err != nil {
		return err
	}

	if jsonOutput {
		type snapshotJSON struct {
			ID           string `json:"id"`
			Label        string `json:"label"`
			Timestamp    int64  `json:"timestamp"`
			AgentName    string `json:"agent_name"`
			FilesChanged int    `json:"files_changed"`
			TotalSize    int64  `json:"total_size"`
			Notes        string `json:"notes"`
		}
		out := make([]snapshotJSON, len(snapshots))
		for i, s := range snapshots {
			out[i] = snapshotJSON{
				ID:           s.ID,
				Label:        s.Label,
				Timestamp:    s.Timestamp,
				AgentName:    s.AgentName,
				FilesChanged: s.FileCount,
				TotalSize:    s.TotalSize,
				Notes:        s.Notes,
			}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(snapshots) == 0 {
		fmt.Printf("No snapshots on branch %s yet. Run `avc snapshot <label>` to create one.\n",
			bold(activeName))
		return nil
	}

	fmt.Printf("Branch: %s\n\n", bold(green(activeName)))
	fmt.Printf("%-20s  %-30s  %-20s  %s\n",
		bold("ID"), bold("Label"), bold("Timestamp"), bold("Files"))
	fmt.Println(dim("------------------------------------------------------------------------------------"))
	for _, s := range snapshots {
		ts := time.Unix(s.Timestamp, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%s  %-30s  %s  %s\n",
			cyan(fmt.Sprintf("%-20s", s.ID)),
			s.Label,
			dim(fmt.Sprintf("%-20s", ts)),
			yellow(fmt.Sprintf("%d", s.FileCount)),
		)
	}
	return nil
}
