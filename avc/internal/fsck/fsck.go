// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fsck verifies object-store integrity: every stored blob must
// decode (raw or zstd) to content whose SHA256 equals its filename. This is
// the audit tool the hot read path deliberately omits — hashing every read
// would double restore cost, so corruption checking is explicit instead.
package fsck

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/objstore"
)

// CorruptObject is one blob whose content no longer matches its hash.
type CorruptObject struct {
	Hash              string   `json:"hash"`
	AffectedSnapshots []string `json:"affected_snapshots"` // snapshot IDs whose files reference this blob
	QuarantinedTo     string   `json:"quarantined_to,omitempty"`
}

// Result summarises an fsck run.
type Result struct {
	ScannedObjects int             `json:"scanned_objects"`
	Corrupt        []CorruptObject `json:"corrupt"`
	Repaired       bool            `json:"repaired"` // true when corrupt objects were quarantined to .avc/corrupt/
}

// Run re-hashes every object in the store. With repair=true, corrupt blobs
// are moved to .avc/corrupt/<hash> — out of the store so nothing dedupes
// against or restores from them — and the snapshots they damage are listed
// so the user knows what history is affected.
func Run(projectRoot string, repair bool) (*Result, error) {
	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	result := &Result{}

	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}

		shard := filepath.Base(filepath.Dir(path))
		hash := shard + filepath.Base(path)
		result.ScannedObjects++

		data, readErr := objstore.Read(projectRoot, hash)
		if readErr == nil {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) == hash {
				return nil // intact
			}
		}

		corrupt := CorruptObject{Hash: hash}
		if repair {
			quarantineDir := filepath.Join(projectRoot, ".avc", "corrupt")
			if err := os.MkdirAll(quarantineDir, 0755); err != nil {
				return err
			}
			dst := filepath.Join(quarantineDir, hash)
			if err := os.Rename(path, dst); err != nil {
				return err
			}
			corrupt.QuarantinedTo = dst
			result.Repaired = true
		}
		result.Corrupt = append(result.Corrupt, corrupt)
		return nil
	})
	if os.IsNotExist(err) {
		return result, nil // no object store yet — vacuously intact
	}
	if err != nil {
		return nil, err
	}

	if len(result.Corrupt) > 0 {
		if err := attachAffectedSnapshots(projectRoot, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// attachAffectedSnapshots maps each corrupt blob to the snapshots whose file
// records reference it, via the files table.
func attachAffectedSnapshots(projectRoot string, result *Result) error {
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	for i := range result.Corrupt {
		snapIDs, err := store.SnapshotsReferencingHash(result.Corrupt[i].Hash)
		if err != nil {
			return err
		}
		result.Corrupt[i].AffectedSnapshots = snapIDs
	}
	return nil
}
