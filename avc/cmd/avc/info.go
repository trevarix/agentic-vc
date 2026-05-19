package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <snapshot_id>",
	Short: "Show detailed metadata for a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE:  runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
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

	snap, err := store.GetSnapshot(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	files, err := store.GetSnapshotFiles(snapshotID)
	if err != nil {
		return err
	}

	if jsonOutput {
		type fileJSON struct {
			Path     string `json:"path"`
			Hash     string `json:"hash"`
			Size     int64  `json:"size"`
		}
		fileList := make([]fileJSON, len(files))
		for i, f := range files {
			fileList[i] = fileJSON{Path: f.RelativePath, Hash: f.FileHash, Size: f.FileSize}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":         snap.ID,
			"label":      snap.Label,
			"timestamp":  snap.Timestamp,
			"agent_name": snap.AgentName,
			"notes":      snap.Notes,
			"file_count": snap.FileCount,
			"total_size": snap.TotalSize,
			"files":      fileList,
		})
	}

	ts := time.Unix(snap.Timestamp, 0).Format("2006-01-02 15:04:05")

	agentVal := dim("—")
	if snap.AgentName != "" {
		agentVal = blue(snap.AgentName)
	}
	notesVal := dim("—")
	if snap.Notes != "" {
		notesVal = snap.Notes
	}

	fmt.Printf("%s %s\n", accent("◆ Snapshot:"), cyan(snap.ID))
	fmt.Println(ruler(60))
	fmt.Printf("  %s %s\n", prop("Label:  "), bold(snap.Label))
	fmt.Printf("  %s %s\n", prop("Created:"), dim(ts))
	fmt.Printf("  %s %s\n", prop("Agent:  "), agentVal)
	fmt.Printf("  %s %s\n", prop("Notes:  "), notesVal)
	fmt.Printf("  %s %s\n", prop("Files:  "), yellow(fmt.Sprintf("%d", snap.FileCount)))
	fmt.Printf("  %s %s\n\n", prop("Size:   "), dim(fmt.Sprintf("%d bytes", snap.TotalSize)))

	fmt.Printf("%s\n", accent("Files"))
	fmt.Println(ruler(60))
	for _, f := range files {
		fmt.Printf("  %s  %s\n", cyan(f.RelativePath), dim(fmt.Sprintf("(%d bytes)", f.FileSize)))
	}
	return nil
}
