// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package snapshot

import (
	"fmt"
	"path/filepath"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
)

// CatFile returns the raw bytes of filePath as stored in snapshotID.
// filePath may use either OS-native or forward-slash separators.
func CatFile(projectRoot, snapshotID, filePath string) ([]byte, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(snapshotID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", snapshotID)
	}

	files, err := store.GetSnapshotFiles(snapshotID)
	if err != nil {
		return nil, err
	}

	normalized := filepath.ToSlash(filePath)
	for _, f := range files {
		if f.RelativePath == normalized {
			return restore.ReadObject(projectRoot, f.FileHash)
		}
	}

	return nil, fmt.Errorf("file '%s' not found in snapshot '%s'", filePath, snapshotID)
}
