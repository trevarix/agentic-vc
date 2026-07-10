// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package oplog records destructive operations (restore, merge, undo)
// together with the safety snapshot that reverses each one. The log is what
// lets `avc undo` work with zero arguments: undoing simply restores the
// newest entry's undo snapshot — and because an undo records itself, running
// it twice acts as redo.
package oplog

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// Operation kinds.
const (
	KindRestore = "restore"
	KindMerge   = "merge"
	KindUndo    = "undo"
)

// Record appends one operation to the log. undoSnapshotID is the snapshot
// that reverses the operation; when empty (a restore of an already-clean
// tree took no safety snapshot), the branch's current HEAD is used — the
// HEAD still describes the pre-restore state, since restores never snapshot.
//
// Recording is deliberately non-fatal for callers: an operation that
// succeeded must not be reported as failed because its log entry could not
// be written, so callers ignore the returned error at their discretion.
func Record(projectRoot, branchID, kind, undoSnapshotID, details string) error {
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return err
	}

	if undoSnapshotID == "" {
		head, err := store.GetHeadSnapshot(branchID)
		if err != nil {
			return err // nothing to undo back to — skip recording
		}
		undoSnapshotID = head.ID
	}

	return store.InsertOperation(&db.Operation{
		ID:             newOpID(),
		ProjectID:      proj.ID,
		BranchID:       branchID,
		Kind:           kind,
		UndoSnapshotID: undoSnapshotID,
		Details:        details,
		CreatedAt:      time.Now().Unix(),
	})
}

func newOpID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "op-" + hex.EncodeToString(b)
}
