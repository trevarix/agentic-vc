// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package snapshot

import (
	"fmt"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
)

// CreateIfDirty snapshots sourceDir only if its contents differ from
// headSnapshotID. Used as a safety net before destructive operations
// (restore, merge) so that un-snapshotted work is never silently lost.
//
// Returns (nil, nil) when the working tree already matches headSnapshotID —
// no snapshot is taken. Pass "" for headSnapshotID when the branch has no
// snapshots yet; CreateIfDirty always snapshots in that case, since "no
// history" and "current state" trivially differ.
func CreateIfDirty(projectRoot, sourceDir, headSnapshotID, label, agentName, notes, branchID string) (*Result, error) {
	if headSnapshotID != "" {
		result, err := diff.CompareWithCurrentDir(projectRoot, sourceDir, headSnapshotID)
		if err == nil && len(result.Files) == 0 {
			return nil, nil // working tree matches HEAD -- nothing to capture
		}
		// On error, fall through and snapshot anyway: an unnecessary snapshot
		// is harmless, but silently skipping one after a failed dirty-check
		// is exactly the data-loss risk this function exists to prevent.
	}
	return Create(projectRoot, label, agentName, notes, branchID, sourceDir)
}

// CreateBeforeRestore captures the working tree in sourceDir if it differs
// from branchID's current HEAD snapshot, before a restore overwrites it. The
// one shared helper behind the pre-restore safety snapshot used by the CLI,
// MCP, and web restore surfaces, and the pre-merge dirty-workspace guard in
// package merge (see docs/plans/02-merge-integrity.md items 3 and 4).
//
// Returns (nil, nil) if the tree is already clean — no snapshot is needed.
func CreateBeforeRestore(projectRoot, sourceDir, branchID, targetSnapshotID string) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	head, headErr := store.GetHeadSnapshot(branchID)
	store.Close()

	headID := ""
	if headErr == nil {
		headID = head.ID
	}
	return CreateIfDirty(
		projectRoot, sourceDir, headID,
		fmt.Sprintf("pre-restore: before restoring %s", targetSnapshotID),
		"avc-restore",
		"automatic safety snapshot captured before a restore",
		branchID,
	)
}
