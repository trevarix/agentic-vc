package avc

import (
	"encoding/json"
	"fmt"
	"os"

	branchpkg "github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
	"github.com/spf13/cobra"
)



var (
	snapshotAgent string
	snapshotNotes string
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot <label>",
	Short: "Create a snapshot of the current project state",
	Long: `Hashes all tracked files and saves a snapshot to the AVC database.
The label is a short human-readable description (e.g. "Before refactor").
The snapshot is associated with the currently active branch.`,
	Args: cobra.ExactArgs(1),
	RunE: runSnapshot,
}

func init() {
	snapshotCmd.Flags().StringVar(&snapshotAgent, "agent", "", "Name of the agent creating this snapshot")
	snapshotCmd.Flags().StringVar(&snapshotNotes, "notes", "", "Optional notes for this snapshot")
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	label := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branchID, err := branchpkg.GetActiveBranchID(projectPath)
	if err != nil {
		return fmt.Errorf("could not determine active branch: %w", err)
	}

	// For non-main branches use the workspace as the source directory so the
	// snapshot captures what the agent actually changed, not the real project root.
	branchName := branchpkg.GetActiveBranchName(projectPath)
	sourceDir := branchpkg.WorkspacePath(projectPath, branchName) // "" for main

	snap, err := snapshot.Create(projectPath, label, snapshotAgent, snapshotNotes, branchID, sourceDir)
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":            snap.ID,
			"label":         snap.Label,
			"timestamp":     snap.Timestamp,
			"agent_name":    snap.AgentName,
			"files_changed": snap.FileCount,
			"total_size":    snap.TotalSize,
			"notes":         snap.Notes,
			"branch_id":     snap.BranchID,
			"success":       true,
		})
	}

	fmt.Printf("%s %s\n", bold("Snapshot created:"), cyan(snap.ID))
	fmt.Printf("  Label:    %s\n", bold(snap.Label))
	fmt.Printf("  Branch:   %s\n", green(branchpkg.GetActiveBranchName(projectPath)))
	fmt.Printf("  Files:    %s\n", yellow(fmt.Sprintf("%d", snap.FileCount)))
	fmt.Printf("  Size:     %s\n", dim(fmt.Sprintf("%d bytes", snap.TotalSize)))
	return nil
}
