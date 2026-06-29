// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package snapshot

import (
	"path/filepath"

	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// FileHistoryEntry describes one snapshot that contains a particular file.
type FileHistoryEntry struct {
	SnapshotID string `json:"snapshot_id"`
	Label      string `json:"label"`
	Timestamp  int64  `json:"timestamp"`
	AgentName  string `json:"agent_name"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
}

// FileHistory returns every snapshot containing filePath, newest first.
func FileHistory(projectRoot, filePath string) ([]*FileHistoryEntry, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	snapshots, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}

	normalized := filepath.ToSlash(filepath.Clean(filePath))
	var history []*FileHistoryEntry

	for _, snap := range snapshots {
		files, err := store.GetSnapshotFiles(snap.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.RelativePath == normalized {
				history = append(history, &FileHistoryEntry{
					SnapshotID: snap.ID,
					Label:      snap.Label,
					Timestamp:  snap.Timestamp,
					AgentName:  snap.AgentName,
					Hash:       f.FileHash,
					Size:       f.FileSize,
				})
				break
			}
		}
	}

	return history, nil
}
