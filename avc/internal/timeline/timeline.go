// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package timeline renders a branch's history as a story: snapshots grouped
// by the agent session that produced them, each with its heuristic change
// summary, interleaved with the destructive operations (restore, merge,
// undo) from the operations log. This is the "what did my agents do while I
// slept" report behind `avc timeline`.
package timeline

import (
	"fmt"
	"slices"
	"sort"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
)

// Event kinds.
const (
	KindSnapshot  = "snapshot"
	KindOperation = "operation"
)

// Event is one timeline entry: either a snapshot or a logged operation.
type Event struct {
	Kind      string `json:"kind"` // "snapshot" | "operation"
	Timestamp int64  `json:"timestamp"`

	// Snapshot fields (Kind == "snapshot").
	SnapshotID string `json:"snapshot_id,omitempty"`
	Label      string `json:"label,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
	FileCount  int    `json:"file_count,omitempty"`
	Summary    string `json:"summary,omitempty"`

	// Operation fields (Kind == "operation").
	OpKind  string `json:"op_kind,omitempty"` // "restore" | "merge" | "undo"
	Details string `json:"details,omitempty"`
}

// Session is one contiguous run of events attributed to the same session ID.
// A session that is interrupted by another session's snapshots appears as
// multiple blocks — the timeline is chronological, not regrouped.
type Session struct {
	SessionID string   `json:"session_id"` // "" = unattributed
	Task      string   `json:"task,omitempty"`
	Agents    []string `json:"agents,omitempty"`
	StartedAt int64    `json:"started_at"`
	EndedAt   int64    `json:"ended_at"`
	Events    []Event  `json:"events"`
}

// Result is the full timeline for one branch.
type Result struct {
	BranchName string    `json:"branch"`
	Sessions   []Session `json:"sessions"`
}

// maxInterleavedOps bounds how many operations-log entries are considered
// for interleaving. The log is small (destructive ops only), so this is a
// generous ceiling, not a pagination mechanism.
const maxInterleavedOps = 500

// Build assembles the timeline for branchName. sessionID, when non-empty,
// restricts the timeline to that session's snapshots (and the operations
// that happened during it). limit caps how many snapshots are shown,
// newest-first before chronological ordering; <= 0 means the default of 50.
func Build(projectRoot, branchName, sessionID string, limit int) (*Result, error) {
	if limit <= 0 {
		limit = 50
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}
	branch, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		return nil, err
	}

	// All snapshots on the branch, newest first, used both for display and
	// to map each snapshot to its predecessor (the summary baseline).
	all, err := store.ListSnapshotsByBranch(branch.ID)
	if err != nil {
		return nil, err
	}
	prevOf := make(map[string]string, len(all)) // snapshot ID → predecessor ID
	for i, s := range all {
		if i+1 < len(all) {
			prevOf[s.ID] = all[i+1].ID
		} else {
			prevOf[s.ID] = branch.BaseSnapshotID // "" when the branch has no base
		}
	}

	// Select the snapshots to display: session filter, then newest `limit`.
	var selected []*db.Snapshot
	for _, s := range all {
		if sessionID != "" && s.SessionID != sessionID {
			continue
		}
		selected = append(selected, s)
		if len(selected) == limit {
			break
		}
	}
	// Reverse to chronological (oldest first) for the story rendering.
	for l, r := 0, len(selected)-1; l < r; l, r = l+1, r-1 {
		selected[l], selected[r] = selected[r], selected[l]
	}

	// Operations to interleave: everything on this branch, plus merges when
	// rendering main — a merge writes to main but is logged under the agent
	// branch that was merged.
	ops, err := store.ListOperations(proj.ID, maxInterleavedOps)
	if err != nil {
		return nil, err
	}
	var opEvents []Event
	for _, op := range ops {
		if op.BranchID != branch.ID && !(branchName == "main" && op.Kind == "merge") {
			continue
		}
		opEvents = append(opEvents, Event{
			Kind:      KindOperation,
			Timestamp: op.CreatedAt,
			OpKind:    op.Kind,
			Details:   op.Details,
		})
	}

	// When a window is in effect (session filter or snapshot limit), clamp
	// operations to it so the timeline doesn't dangle ops with no context.
	if len(selected) > 0 && (sessionID != "" || len(selected) < len(all)) {
		lo, hi := selected[0].Timestamp, selected[len(selected)-1].Timestamp
		kept := opEvents[:0]
		for _, e := range opEvents {
			if e.Timestamp >= lo && (sessionID == "" || e.Timestamp <= hi) {
				kept = append(kept, e)
			}
		}
		opEvents = kept
	}

	// Build snapshot events with summaries.
	events := make([]Event, 0, len(selected)+len(opEvents))
	for _, s := range selected {
		events = append(events, Event{
			Kind:       KindSnapshot,
			Timestamp:  s.Timestamp,
			SnapshotID: s.ID,
			Label:      s.Label,
			AgentName:  s.AgentName,
			FileCount:  s.FileCount,
			Summary:    summaryFor(projectRoot, store, s, prevOf[s.ID]),
		})
	}
	events = append(events, opEvents...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp != events[j].Timestamp {
			return events[i].Timestamp < events[j].Timestamp
		}
		// Same second: a snapshot precedes the operation it enabled
		// (safety snapshots are taken before their op is logged).
		return events[i].Kind == KindSnapshot && events[j].Kind == KindOperation
	})

	// Group chronological events into session blocks. Operations attach to
	// the block in progress when they happened.
	sessionOf := make(map[string]*db.Snapshot, len(selected))
	for _, s := range selected {
		sessionOf[s.ID] = s
	}
	var sessions []Session
	for _, e := range events {
		snapSession, snapTask := "", ""
		if e.Kind == KindSnapshot {
			s := sessionOf[e.SnapshotID]
			snapSession, snapTask = s.SessionID, s.Task
		}
		startNew := len(sessions) == 0 ||
			(e.Kind == KindSnapshot && snapSession != sessions[len(sessions)-1].SessionID)
		if startNew {
			sessions = append(sessions, Session{SessionID: snapSession, StartedAt: e.Timestamp})
		}
		cur := &sessions[len(sessions)-1]
		cur.Events = append(cur.Events, e)
		cur.EndedAt = e.Timestamp
		if cur.Task == "" && snapTask != "" {
			cur.Task = snapTask
		}
		if e.AgentName != "" && !slices.Contains(cur.Agents, e.AgentName) {
			cur.Agents = append(cur.Agents, e.AgentName)
		}
	}

	return &Result{BranchName: branchName, Sessions: sessions}, nil
}

// summaryFor returns the change summary for snap against its predecessor,
// reading the persisted per-file fragments when present and computing (and
// caching) them lazily otherwise. Best-effort — an empty summary never
// fails the timeline.
func summaryFor(projectRoot string, store *db.Store, snap *db.Snapshot, prevID string) string {
	if prevID == "" {
		return fmt.Sprintf("initial snapshot (%d files)", snap.FileCount)
	}
	if rows, err := store.GetDiffCache(prevID, snap.ID); err == nil && len(rows) > 0 {
		return diffpkg.SummarizeCached(rows)
	}
	files, err := diffpkg.CacheSummaries(projectRoot, prevID, snap.ID)
	if err != nil {
		return ""
	}
	return diffpkg.Summarize(files)
}
