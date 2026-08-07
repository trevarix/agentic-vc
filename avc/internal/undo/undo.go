// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package undo reverses the most recent destructive operation (restore or
// merge) using the operations log, with zero arguments. Undoing an undo
// restores the state the first undo replaced — i.e. redo — because every
// undo records itself back into the same log.
package undo

import (
	"fmt"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/oplog"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// Result describes what an Undo reversed and how to reverse the undo itself.
type Result struct {
	UndoneKind         string // kind of the operation that was reversed
	UndoneDetails      string // its human-readable summary
	RestoredSnapshotID string // snapshot the working tree was restored to
	RedoSnapshotID     string // restoring this snapshot reverses the undo (also reachable by running undo again)
	BranchName         string // branch whose working tree was restored
	ReactivatedBranch  string // agent branch marked active again (merge undo only)
}

// Undo reverses the newest operation in the log.
func Undo(projectRoot string) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		return nil, err
	}
	op, err := store.GetLastOperation(proj.ID)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("nothing to undo: %w", err)
	}

	// The undo snapshot's own branch tells us which working tree to restore:
	// the project root for main (e.g. a pre-merge snapshot), the workspace
	// for an agent branch.
	undoSnap, err := store.GetSnapshot(op.UndoSnapshotID)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("the snapshot that would undo this operation no longer exists: %w", err)
	}
	targetBranch, err := store.GetBranchByID(undoSnap.BranchID)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("resolve branch of undo snapshot: %w", err)
	}
	store.Close()

	targetDir := branch.WorkspacePath(projectRoot, targetBranch.Name)
	if targetDir == "" {
		targetDir = projectRoot
	}

	// Capture the current state first so the undo itself is reversible.
	redoSnap, err := snapshot.CreateBeforeRestore(projectRoot, targetDir, targetBranch.ID, op.UndoSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("pre-undo safety snapshot failed (undo aborted to avoid data loss): %w", err)
	}
	redoSnapID := ""
	if redoSnap != nil {
		redoSnapID = redoSnap.ID
	}

	if _, err := restore.RestoreToDir(projectRoot, op.UndoSnapshotID, targetDir); err != nil {
		return nil, fmt.Errorf("restore undo snapshot: %w", err)
	}

	result := &Result{
		UndoneKind:         op.Kind,
		UndoneDetails:      op.Details,
		RestoredSnapshotID: op.UndoSnapshotID,
		RedoSnapshotID:     redoSnapID,
		BranchName:         targetBranch.Name,
	}

	// Undoing a merge also reactivates the agent branch and rebuilds its
	// workspace from the branch HEAD, so work can resume where it left off
	// (the workspace was deleted when the merge completed).
	if op.Kind == oplog.KindMerge && op.BranchID != "" {
		if store2, err2 := db.Open(projectRoot); err2 == nil {
			if agentBranch, bErr := store2.GetBranchByID(op.BranchID); bErr == nil {
				_ = store2.SetBranchStatus(agentBranch.ID, "active")
				if head, hErr := store2.GetHeadSnapshot(agentBranch.ID); hErr == nil {
					if ws := branch.WorkspacePath(projectRoot, agentBranch.Name); ws != "" {
						_, _ = restore.RestoreToDir(projectRoot, head.ID, ws)
					}
				}
				result.ReactivatedBranch = agentBranch.Name
			}
			store2.Close()
		}
	}

	// Record the undo so running undo again acts as redo. Best-effort: the
	// undo already succeeded.
	_ = oplog.Record(projectRoot, targetBranch.ID, oplog.KindUndo, redoSnapID,
		fmt.Sprintf("undid %s (%s)", op.Kind, op.Details))

	return result, nil
}

// List returns the newest operations, most recent first.
func List(projectRoot string, limit int) ([]*db.Operation, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}
	return store.ListOperations(proj.ID, limit)
}
