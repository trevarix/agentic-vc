// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agenthook handles callbacks from an agent harness's hook system.
// Claude Code, Windsurf, Codex and Copilot invoke `avc hook pre-edit` from a
// pre-tool or pre-write hook before the agent edits a file; this package
// decides whether that warrants a snapshot.
//
// The unit of work is the session, not the edit. A checkpoint per edit would
// flood `avc list` and leave `avc timeline` unreadable, so PreEdit takes at
// most one snapshot per agent session: the state of the project before that
// session touched anything.
package agenthook

import (
	"fmt"
	"os"
	"path/filepath"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// Action values reported in a Result.
const (
	ActionSnapshotted = "snapshotted"
	ActionSkipped     = "skipped"
)

// Skip reasons. These are ordinary outcomes, not failures — a hook that fires
// outside an AVC project or twice in one session has nothing to do.
const (
	ReasonNoProject     = "not an AVC project"
	ReasonNoSession     = "hook payload carried no session_id"
	ReasonAlreadyExists = "session already has a checkpoint"
)

// checkpointLabel marks hook-created snapshots. The auto: prefix is the same
// convention agents use, so `avc list` groups them with other agent activity.
const checkpointLabel = "auto: session start checkpoint"

// defaultHarness names the harness when the hook command does not say which
// one invoked it — Claude Code, which was the first to call this hook.
const defaultHarness = "claude-code"

// checkpointAgentSuffix attributes hook snapshots to the harness rather than
// to the model, since no agent chose to take them: "<harness>-hook".
const checkpointAgentSuffix = "-hook"

// Input is the subset of a harness hook payload that AVC reads. ParseInput
// fills it from any supported harness's field names.
type Input struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	// Harness names the agent tool that ran the hook (claude-code, windsurf,
	// codex, copilot). Set from the hook command line, not the payload.
	Harness string `json:"-"`
}

// Result describes what PreEdit did.
type Result struct {
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	Label      string `json:"label,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Project    string `json:"project,omitempty"`
}

// PreEdit takes one checkpoint snapshot per agent session.
//
// It returns a skip Result rather than an error for the ordinary cases — no
// project, no session ID, session already checkpointed — so the caller can
// distinguish "nothing to do" from a real failure.
func PreEdit(in Input) (Result, error) {
	root := findProjectRoot(in.CWD)
	if root == "" {
		return Result{Action: ActionSkipped, Reason: ReasonNoProject}, nil
	}

	// Without a session ID there is no way to tell a session's first edit from
	// its hundredth, and snapshotting every edit is the outcome this package
	// exists to avoid.
	if in.SessionID == "" {
		return Result{Action: ActionSkipped, Reason: ReasonNoSession, Project: root}, nil
	}

	seen, err := sessionCheckpointed(root, in.SessionID)
	if err != nil {
		return Result{}, err
	}
	if seen {
		return Result{
			Action:    ActionSkipped,
			Reason:    ReasonAlreadyExists,
			SessionID: in.SessionID,
			Project:   root,
		}, nil
	}

	branchID, err := branchpkg.GetActiveBranchID(root)
	if err != nil {
		return Result{}, fmt.Errorf("determine active branch: %w", err)
	}

	// On a non-main branch the workspace is the source of truth, matching what
	// `avc snapshot` captures there.
	branchName := branchpkg.GetActiveBranchName(root)
	sourceDir := branchpkg.WorkspacePath(root, branchName) // "" for main

	snap, err := snapshot.CreateWithOptions(root, snapshot.Options{
		Label:     checkpointLabel,
		AgentName: harnessName(in.Harness) + checkpointAgentSuffix,
		BranchID:  branchID,
		SourceDir: sourceDir,
		SessionID: in.SessionID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("snapshot failed: %w", err)
	}

	return Result{
		Action:     ActionSnapshotted,
		SnapshotID: snap.ID,
		Label:      snap.Label,
		SessionID:  in.SessionID,
		Project:    root,
	}, nil
}

// harnessName returns the harness to attribute a checkpoint to.
func harnessName(h string) string {
	if h == "" {
		return defaultHarness
	}
	return h
}

// sessionCheckpointed reports whether any snapshot already carries this
// session ID. The store is opened and closed here so the snapshot that may
// follow gets its own connection.
func sessionCheckpointed(projectRoot, sessionID string) (bool, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return false, fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	snaps, err := store.ListSnapshotsFiltered(db.SnapshotFilter{
		SessionID: sessionID,
		Limit:     1,
	})
	if err != nil {
		return false, fmt.Errorf("look up session snapshots: %w", err)
	}
	return len(snaps) > 0, nil
}

// findProjectRoot walks up from start looking for .avc, returning "" when the
// path is not inside an AVC project. An empty start falls back to the working
// directory, which is what a harness that sends no cwd implies.
func findProjectRoot(start string) string {
	dir := start
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		dir = cwd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	dir = abs

	for {
		if info, err := os.Stat(filepath.Join(dir, ".avc")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
