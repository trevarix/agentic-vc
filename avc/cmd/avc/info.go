// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

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
	fmt.Printf("Snapshot: %s\n", snap.ID)
	fmt.Printf("  Label:     %s\n", snap.Label)
	fmt.Printf("  Created:   %s\n", ts)
	fmt.Printf("  Agent:     %s\n", snap.AgentName)
	fmt.Printf("  Notes:     %s\n", snap.Notes)
	fmt.Printf("  Files:     %d\n", snap.FileCount)
	fmt.Printf("  Size:      %d bytes\n\n", snap.TotalSize)
	fmt.Println("Files:")
	for _, f := range files {
		fmt.Printf("  %s  (%d bytes)\n", f.RelativePath, f.FileSize)
	}
	return nil
}
