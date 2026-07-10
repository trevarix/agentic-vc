// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests for docs/plans/01-data-safety.md — each test mirrors a
// reproduction from the 2026-07 code review.
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/gc"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/trash"
)

// ─── 1 · Restore must not delete ignored/untracked files (review 1.1) ────────

// TestRestore_PreservesIgnoredFiles reproduces the review's data-loss
// scenario: a restore must never remove a file matched by .avcignore.
func TestRestore_PreservesIgnoredFiles(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, ".avcignore", "*.env\n")
	writeFile(t, projectRoot, "code.txt", "hello")
	writeFile(t, projectRoot, "prod.env", "SECRET_KEY=super-secret")

	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if _, err := restore.Restore(projectRoot, snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, "prod.env")); err != nil {
		t.Errorf("prod.env should survive restore (it is .avcignore'd), got: %v", err)
	}
}

// TestRestore_QuarantinesUntrackedFiles verifies that a file which is not
// ignored but also not part of the target snapshot is moved to trash instead
// of being permanently deleted.
func TestRestore_QuarantinesUntrackedFiles(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "code.txt", "hello")

	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	writeFile(t, projectRoot, "untracked.txt", "should be quarantined, not deleted")

	result, err := restore.Restore(projectRoot, snap.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if result.QuarantinedFiles != 1 {
		t.Errorf("QuarantinedFiles = %d, want 1", result.QuarantinedFiles)
	}
	if result.TrashOpID == "" {
		t.Error("expected a non-empty TrashOpID after quarantining a file")
	}

	if _, err := os.Stat(filepath.Join(projectRoot, "untracked.txt")); !os.IsNotExist(err) {
		t.Errorf("untracked.txt should no longer be in the working tree, stat err = %v", err)
	}

	entries, err := trash.List(projectRoot)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 trash entry, got %d", len(entries))
	}
	if len(entries[0].Files) != 1 || entries[0].Files[0] != "untracked.txt" {
		t.Errorf("trash entry files = %v, want [untracked.txt]", entries[0].Files)
	}
}

// TestTrash_Empty verifies that avc trash empty permanently removes entries.
func TestTrash_Empty(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "code.txt", "hello")
	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	writeFile(t, projectRoot, "untracked.txt", "quarantine me")
	if _, err := restore.Restore(projectRoot, snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	removed, err := trash.Empty(projectRoot, 0)
	if err != nil {
		t.Fatalf("trash.Empty: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	entries, err := trash.List(projectRoot)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected trash to be empty after Empty(0), got %d entries", len(entries))
	}
}

// ─── 2 · Never hardlink into workspaces (review 1.4) ──────────────────────────

// TestWorkspace_NoHardlinkAliasing reproduces the review's isolation-breaking
// scenario: a branch created before main has any snapshot copies project
// files via hardlink today, so an in-place write in the workspace mutates the
// real project root. This must no longer happen.
func TestWorkspace_NoHardlinkAliasing(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t) // main has zero snapshots
	writeFile(t, projectRoot, "data.txt", "original")

	b, err := branch.Create(projectRoot, "hardlink-branch", "")
	if err != nil {
		t.Fatalf("branch.Create: %v", err)
	}

	ws := branch.WorkspacePath(projectRoot, b.Name)
	wsFile := filepath.Join(ws, "data.txt")

	f, err := os.OpenFile(wsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open workspace file: %v", err)
	}
	if _, err := f.WriteString("\nMUTATED IN WORKSPACE"); err != nil {
		f.Close()
		t.Fatalf("append to workspace file: %v", err)
	}
	f.Close()

	data, err := os.ReadFile(filepath.Join(projectRoot, "data.txt"))
	if err != nil {
		t.Fatalf("read project-root file: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("project-root file was mutated by a workspace write (hardlink aliasing): %q", data)
	}
}

// ─── 3 · Atomic object-store writes (review 2.1) ──────────────────────────────

// TestStoreObject_StrayTempFileNeverMasksRealObject verifies that a leftover
// .tmp file from a previously interrupted write does not interfere with a
// later, successful StoreObject call for the same hash.
func TestStoreObject_StrayTempFileNeverMasksRealObject(t *testing.T) {
	projectRoot := setupTestProject(t)
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := []byte("hello world")

	objPath := filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
	if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Simulate a stray leftover temp file from a previous interrupted write.
	if err := os.WriteFile(objPath+".99999.tmp", []byte("leftover garbage"), 0644); err != nil {
		t.Fatalf("write stray tmp: %v", err)
	}

	if err := restore.StoreObject(projectRoot, hash, data); err != nil {
		t.Fatalf("StoreObject: %v", err)
	}

	got, err := restore.ReadObject(projectRoot, hash)
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("stored object content = %q, want %q", got, data)
	}
}

// TestReadObject_EmptyHashReturnsErrorNotPanic covers the empty-hash guard
// shared with the merge-side fix in Plan 02 (review 1.2's panic root cause).
func TestReadObject_EmptyHashReturnsErrorNotPanic(t *testing.T) {
	projectRoot := setupTestProject(t)
	if _, err := restore.ReadObject(projectRoot, ""); err == nil {
		t.Error("expected an error for an empty object hash, got nil")
	}
}

// TestDiffReadObjectSafe_EmptyHashNoPanic covers the diff package's read path,
// which returns nil on any error rather than propagating one.
func TestDiffReadObjectSafe_EmptyHashNoPanic(t *testing.T) {
	projectRoot := setupTestProject(t)
	if data := diff.ReadObjectSafe(projectRoot, ""); data != nil {
		t.Errorf("expected nil for an empty hash, got %v", data)
	}
}

// TestGC_SweepsStrayTempFiles verifies gc.Run treats leftover *.tmp files as
// always-safe-to-delete, regardless of the live-hash set.
func TestGC_SweepsStrayTempFiles(t *testing.T) {
	projectRoot := setupTestProject(t)
	tmpPath := filepath.Join(projectRoot, ".avc", "objects", "ab", "cdef0123.12345.tmp")
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("write stray tmp: %v", err)
	}

	result, err := gc.RunWithGrace(projectRoot, false, 0)
	if err != nil {
		t.Fatalf("gc.Run: %v", err)
	}
	if result.DeletedObjects < 1 {
		t.Errorf("expected the stray tmp file to be counted as deleted, got %d", result.DeletedObjects)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("stray tmp file should have been removed by gc, stat err = %v", err)
	}
}

// ─── 4 · Transactional snapshot insert (review 2.2) ───────────────────────────

// TestDB_SnapshotInsertIsAtomic verifies that a failure partway through
// InsertSnapshotWithFiles (here, a duplicate file primary key) leaves no
// partially-written snapshot row behind.
func TestDB_SnapshotInsertIsAtomic(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}

	snap := &db.Snapshot{
		ID:        "snap-atomic-test",
		ProjectID: proj.ID,
		Timestamp: 1,
		Label:     "atomic-test",
		BranchID:  mainBranchID,
		FileCount: 2,
	}
	// Two file rows sharing the same primary key ID — the second insert
	// violates the PK constraint and must roll back the whole transaction.
	files := []*db.File{
		{ID: "file-dup", SnapshotID: snap.ID, RelativePath: "a.txt", FileHash: "h1", FileSize: 1},
		{ID: "file-dup", SnapshotID: snap.ID, RelativePath: "b.txt", FileHash: "h2", FileSize: 1},
	}

	if err := store.InsertSnapshotWithFiles(snap, files); err == nil {
		t.Fatal("expected an error from the duplicate file ID, got nil")
	}

	if _, err := store.GetSnapshot(snap.ID); err == nil {
		t.Error("snapshot row should not exist after a failed insert — the transaction should have rolled back")
	}
}

// ─── 5 · busy_timeout pragma (review 2.6) ─────────────────────────────────────

// TestDB_ConcurrentSnapshots_NoBusyErrors exercises the realistic multi-writer
// scenario (CLI, MCP server, extension, and web UI can all call snapshot.Create
// concurrently) and asserts busy_timeout lets writers queue instead of failing.
func TestDB_ConcurrentSnapshots_NoBusyErrors(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	const workers = 4
	const perWorker = 5

	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				path := filepath.Join(projectRoot, fmt.Sprintf("worker-%d-file-%d.txt", worker, i))
				if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
					errCh <- err
					continue
				}
				label := fmt.Sprintf("w%d-%d", worker, i)
				if _, err := snapshot.Create(projectRoot, label, "", "", mainBranchID, ""); err != nil {
					errCh <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent snapshot failed: %v", err)
	}
}
