// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for the PreToolUse hook handler behind `avc hook pre-edit`. The
// contract that matters is restraint: one checkpoint per agent session, and
// silence everywhere else.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/agenthook"
	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// TestPreEditCheckpointsOncePerSession is the core guarantee. A session's
// first edit earns a snapshot; every later edit in that session must not,
// or `avc list` fills with noise and `avc timeline` stops being readable.
func TestPreEditCheckpointsOncePerSession(t *testing.T) {
	project, _ := setupProjectWithMain(t)
	in := agenthook.Input{SessionID: "sess-alpha", CWD: project, ToolName: "Edit"}

	first, err := agenthook.PreEdit(in)
	if err != nil {
		t.Fatalf("first PreEdit: %v", err)
	}
	if first.Action != agenthook.ActionSnapshotted {
		t.Fatalf("first edit of a session: got action %q (%s), want %q",
			first.Action, first.Reason, agenthook.ActionSnapshotted)
	}
	if first.SnapshotID == "" {
		t.Error("snapshotted result carries no snapshot ID")
	}

	for i := 0; i < 3; i++ {
		again, err := agenthook.PreEdit(in)
		if err != nil {
			t.Fatalf("repeat PreEdit %d: %v", i, err)
		}
		if again.Action != agenthook.ActionSkipped {
			t.Fatalf("repeat edit %d: got action %q, want %q", i, again.Action, agenthook.ActionSkipped)
		}
		if again.Reason != agenthook.ReasonAlreadyExists {
			t.Errorf("repeat edit %d: got reason %q, want %q", i, again.Reason, agenthook.ReasonAlreadyExists)
		}
	}

	// Exactly one snapshot may carry the session ID.
	store, err := db.Open(project)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	snaps, err := store.ListSnapshotsFiltered(db.SnapshotFilter{SessionID: "sess-alpha", Limit: -1})
	if err != nil {
		t.Fatalf("list session snapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("session produced %d snapshots, want exactly 1", len(snaps))
	}
}

// TestPreEditSeparateSessionsEachCheckpoint confirms the dedupe is scoped to
// the session and does not suppress the next session's checkpoint.
func TestPreEditSeparateSessionsEachCheckpoint(t *testing.T) {
	project, _ := setupProjectWithMain(t)

	for _, session := range []string{"sess-one", "sess-two"} {
		got, err := agenthook.PreEdit(agenthook.Input{SessionID: session, CWD: project})
		if err != nil {
			t.Fatalf("PreEdit for %s: %v", session, err)
		}
		if got.Action != agenthook.ActionSnapshotted {
			t.Errorf("session %s: got action %q (%s), want %q",
				session, got.Action, got.Reason, agenthook.ActionSnapshotted)
		}
	}
}

// TestPreEditSkipsOutsideProject covers the common case of the hook firing in
// a directory that has never seen `avc init`. It must be a silent no-op, not
// an error the harness surfaces on every edit.
func TestPreEditSkipsOutsideProject(t *testing.T) {
	got, err := agenthook.PreEdit(agenthook.Input{SessionID: "sess-x", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("PreEdit outside a project returned an error: %v", err)
	}
	if got.Action != agenthook.ActionSkipped {
		t.Errorf("got action %q, want %q", got.Action, agenthook.ActionSkipped)
	}
	if got.Reason != agenthook.ReasonNoProject {
		t.Errorf("got reason %q, want %q", got.Reason, agenthook.ReasonNoProject)
	}
}

// TestPreEditSkipsWithoutSessionID guards the fallback. With no session ID
// there is no way to tell a first edit from a hundredth, so taking a snapshot
// would reintroduce exactly the per-edit flooding this handler prevents.
func TestPreEditSkipsWithoutSessionID(t *testing.T) {
	project, _ := setupProjectWithMain(t)

	got, err := agenthook.PreEdit(agenthook.Input{CWD: project})
	if err != nil {
		t.Fatalf("PreEdit without a session ID: %v", err)
	}
	if got.Action != agenthook.ActionSkipped {
		t.Errorf("got action %q, want %q", got.Action, agenthook.ActionSkipped)
	}
	if got.Reason != agenthook.ReasonNoSession {
		t.Errorf("got reason %q, want %q", got.Reason, agenthook.ReasonNoSession)
	}

	store, err := db.Open(project)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	snaps, err := store.ListSnapshots()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("a sessionless hook created %d snapshots, want 0", len(snaps))
	}
}

// TestPreEditFindsProjectFromSubdirectory covers the harness reporting a cwd
// below the project root, which is normal when the agent edits a nested file.
func TestPreEditFindsProjectFromSubdirectory(t *testing.T) {
	project, _ := setupProjectWithMain(t)
	nested := filepath.Join(project, "src", "deep")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	got, err := agenthook.PreEdit(agenthook.Input{SessionID: "sess-nested", CWD: nested})
	if err != nil {
		t.Fatalf("PreEdit from subdirectory: %v", err)
	}
	if got.Action != agenthook.ActionSnapshotted {
		t.Fatalf("got action %q (%s), want %q", got.Action, got.Reason, agenthook.ActionSnapshotted)
	}
	if got.Project != project {
		t.Errorf("resolved project %q, want %q", got.Project, project)
	}
}
