// Package tests — storage management tests.
//
// Covers:
//   - Cascade delete snapshots on branch delete
//   - Object-store garbage collection (gc.Run)
//   - Storage accounting (LiveHashes, object/workspace sizes)
//   - Snapshot retention policy enforcement
package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/gc"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// ─── 2.1 · Cascade delete ────────────────────────────────────────────────────

// TestBranchDelete_CascadesSnapshots verifies that branch.Delete (without
// --keep-history) removes all snapshot and file rows for the deleted branch.
func TestBranchDelete_CascadesSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "a.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/cascade", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "a.go", "branch-v1")
	_, err = snapshot.Create(projectRoot, "branch-snap-1", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot 1: %v", err)
	}
	writeFile(t, ws, "a.go", "branch-v2")
	_, err = snapshot.Create(projectRoot, "branch-snap-2", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("branch snapshot 2: %v", err)
	}

	// Confirm snapshots exist before deletion.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	snapsBeforeDelete, _ := store.ListSnapshotsByBranch(b.ID)
	store.Close()
	if len(snapsBeforeDelete) != 2 {
		t.Fatalf("expected 2 snapshots before delete, got %d", len(snapsBeforeDelete))
	}

	// Delete the branch without --keep-history.
	if err := branch.Switch(projectRoot, "main"); err != nil {
		t.Fatalf("switch to main: %v", err)
	}
	if err := branch.Delete(projectRoot, "feat/cascade", false); err != nil {
		t.Fatalf("branch.Delete: %v", err)
	}

	// Snapshot rows must be gone.
	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db after delete: %v", err)
	}
	defer store2.Close()

	snapsAfter, _ := store2.ListSnapshotsByBranch(b.ID)
	if len(snapsAfter) != 0 {
		t.Errorf("expected 0 snapshots after cascade delete, got %d", len(snapsAfter))
	}
}

// TestBranchDelete_KeepHistory verifies that --keep-history retains snapshot rows.
func TestBranchDelete_KeepHistory(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "b.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/keephistory", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "b.go", "branch content")
	if _, err := snapshot.Create(projectRoot, "kept-snap", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}
	if err := branch.Switch(projectRoot, "main"); err != nil {
		t.Fatalf("switch to main: %v", err)
	}
	if err := branch.Delete(projectRoot, "feat/keephistory", true); err != nil {
		t.Fatalf("branch.Delete(keepHistory=true): %v", err)
	}

	// Snapshot rows must still be present. With --keep-history the branch_id
	// is NULLed out so ListSnapshotsByBranch returns nothing — use ListSnapshots
	// (all snapshots, no branch filter) and look for the retained label.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	all, _ := store.ListSnapshots()
	found := false
	for _, s := range all {
		if s.Label == "kept-snap" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected snapshot rows to be retained with --keep-history, but 'kept-snap' not found")
	}
}

// ─── 2.2 · Garbage collection ────────────────────────────────────────────────

// TestGC_DryRun verifies that a dry run identifies orphaned objects but does
// not delete them.
func TestGC_DryRun(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "gc.go", "original content for gc test\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "snap-to-delete")

	// Delete the snapshot — its objects become orphans.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.DeleteSnapshot(snap.ID); err != nil {
		store.Close()
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	store.Close()

	result, err := gc.RunWithGrace(projectRoot, true /* dryRun */, 0)
	if err != nil {
		t.Fatalf("gc.Run dry-run: %v", err)
	}
	if !result.DryRun {
		t.Error("result.DryRun should be true")
	}
	if result.DeletedObjects == 0 {
		t.Error("expected at least 1 orphaned object identified in dry run")
	}

	// Objects must still exist on disk.
	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	remaining := countFiles(t, objectsDir)
	if remaining == 0 {
		t.Error("dry run should not delete objects — but none remain")
	}
}

// TestGC_Run verifies that --run actually deletes orphaned objects.
func TestGC_Run(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "gc2.go", "content to be orphaned by delete\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "snap-for-gc")

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	beforeCount := countFiles(t, objectsDir)
	if beforeCount == 0 {
		t.Fatal("no objects stored after snapshot")
	}

	// Delete the snapshot — orphan its blobs.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.DeleteSnapshot(snap.ID); err != nil {
		store.Close()
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	store.Close()

	result, err := gc.RunWithGrace(projectRoot, false /* run */, 0)
	if err != nil {
		t.Fatalf("gc.Run: %v", err)
	}
	if result.DryRun {
		t.Error("result.DryRun should be false")
	}
	if result.DeletedObjects == 0 {
		t.Error("expected GC to delete at least one object")
	}

	afterCount := countFiles(t, objectsDir)
	if afterCount >= beforeCount {
		t.Errorf("object count after GC (%d) should be less than before (%d)", afterCount, beforeCount)
	}
}

// TestGC_LiveObjectsArePreserved verifies that objects still referenced by an
// active snapshot are never deleted.
func TestGC_LiveObjectsArePreserved(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "live.go", "live content — must survive GC\n")
	createMainSnap(t, projectRoot, mainBranchID, "live-snap")

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	before := countFiles(t, objectsDir)

	result, err := gc.RunWithGrace(projectRoot, false, 0)
	if err != nil {
		t.Fatalf("gc.Run: %v", err)
	}
	if result.DeletedObjects > 0 {
		t.Errorf("GC deleted %d objects even though all are live", result.DeletedObjects)
	}

	after := countFiles(t, objectsDir)
	if after != before {
		t.Errorf("object count changed from %d to %d — live objects were removed", before, after)
	}
}

// TestGC_EmptyObjectStore verifies that GC on a project with no objects does
// not error.
func TestGC_EmptyObjectStore(t *testing.T) {
	projectRoot := setupTestProject(t)

	result, err := gc.RunWithGrace(projectRoot, true, 0)
	if err != nil {
		t.Fatalf("gc.Run on empty object store: %v", err)
	}
	if result.ScannedObjects != 0 {
		t.Errorf("scanned %d objects in empty store, want 0", result.ScannedObjects)
	}
}

// ─── 2.3 · LiveHashes correctness ────────────────────────────────────────────

// TestLiveHashes_ReflectsActiveSnapshots verifies that LiveHashes returns the
// hashes of all files currently stored in the DB.
func TestLiveHashes_ReflectsActiveSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "lh.go", "content for live-hashes test\n")
	createMainSnap(t, projectRoot, mainBranchID, "lh-snap")

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	live, err := store.LiveHashes()
	if err != nil {
		t.Fatalf("LiveHashes: %v", err)
	}
	if len(live) == 0 {
		t.Error("LiveHashes returned empty set for a project with snapshots")
	}
}

// TestLiveHashes_ExcludesDeletedSnapshots verifies that after a snapshot is
// deleted its hashes are no longer in LiveHashes.
func TestLiveHashes_ExcludesDeletedSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "unique.go", "content-only-in-this-snap\n")
	snap := createMainSnap(t, projectRoot, mainBranchID, "unique-snap")

	// Collect hashes before deletion.
	store, _ := db.Open(projectRoot)
	before, _ := store.LiveHashes()
	store.Close()

	// Delete the snapshot.
	store2, _ := db.Open(projectRoot)
	_ = store2.DeleteSnapshot(snap.ID)
	store2.Close()

	// Hashes from the deleted snapshot must no longer be live.
	store3, _ := db.Open(projectRoot)
	after, _ := store3.LiveHashes()
	store3.Close()

	if len(before) > 0 && len(after) >= len(before) {
		t.Errorf("LiveHashes count did not decrease after snapshot delete: before=%d after=%d",
			len(before), len(after))
	}
}

// ─── 2.4 · Retention policy ──────────────────────────────────────────────────

// TestRetention_PrunesOldestWhenCountExceeded verifies that Enforce deletes
// the oldest snapshots when max_snapshots_per_branch is exceeded.
func TestRetention_PrunesOldestWhenCountExceeded(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	// Create 5 snapshots, one per second (timestamps must be distinct).
	for i := 1; i <= 5; i++ {
		writeFile(t, projectRoot, "file.go", strings.Repeat("x", i))
		createMainSnap(t, projectRoot, mainBranchID, "snap")
		if i < 5 {
			time.Sleep(time.Second)
		}
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	before, _ := store.ListSnapshotsByBranch(mainBranchID)
	store.Close()
	if len(before) < 5 {
		t.Skipf("expected 5 snapshots, got %d — skipping retention test", len(before))
	}

	cfg := &config.RetentionConfig{
		MaxSnapshotsPerBranch: 3,
		AutoGC:                false,
	}
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce: %v", err)
	}

	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db after retention: %v", err)
	}
	defer store2.Close()

	after, _ := store2.ListSnapshotsByBranch(mainBranchID)
	if len(after) > 3 {
		t.Errorf("expected at most 3 snapshots after retention, got %d", len(after))
	}

	// Pruning message must have been written to stderr.
	if !strings.Contains(buf.String(), "Pruned") {
		t.Errorf("expected pruning message in stderr, got %q", buf.String())
	}
}

// TestRetention_PrunesOldSnapshots verifies that Enforce respects MaxAgeDays.
func TestRetention_PrunesOldSnapshots(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	// Create a snapshot now.
	writeFile(t, projectRoot, "age.go", "fresh content")
	freshSnap := createMainSnap(t, projectRoot, mainBranchID, "fresh")
	_ = freshSnap

	// Manually insert an "old" snapshot directly into the DB with a timestamp
	// well beyond the retention cutoff.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	proj, _ := store.GetProject(projectRoot)
	oldTimestamp := time.Now().AddDate(0, 0, -100).Unix() // 100 days ago
	oldSnap := &db.Snapshot{
		ID:        "snap-old-retention-test",
		ProjectID: proj.ID,
		Timestamp: oldTimestamp,
		Label:     "old-snap",
		BranchID:  mainBranchID,
		FileCount: 0,
		TotalSize: 0,
	}
	if err := store.InsertSnapshotWithFiles(oldSnap, nil); err != nil {
		store.Close()
		t.Fatalf("insert old snapshot: %v", err)
	}
	store.Close()

	cfg := &config.RetentionConfig{
		MaxAgeDays: 30, // delete anything older than 30 days
		AutoGC:     false,
	}
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce: %v", err)
	}

	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db after retention: %v", err)
	}
	defer store2.Close()

	// The 100-day-old snapshot must be gone.
	_, err = store2.GetSnapshot("snap-old-retention-test")
	if err == nil {
		t.Error("old snapshot should have been pruned by MaxAgeDays=30, but it still exists")
	}
}

// TestRetention_NoPolicyIsNoop verifies that Enforce with a zero-value config
// does not delete anything.
func TestRetention_NoPolicyIsNoop(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "noop.go", "v1")
	createMainSnap(t, projectRoot, mainBranchID, "snap")

	store, _ := db.Open(projectRoot)
	before, _ := store.ListSnapshotsByBranch(mainBranchID)
	store.Close()

	cfg := &config.RetentionConfig{} // zero-value — no policy
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce with no policy: %v", err)
	}

	store2, _ := db.Open(projectRoot)
	after, _ := store2.ListSnapshotsByBranch(mainBranchID)
	store2.Close()

	if len(after) != len(before) {
		t.Errorf("retention with no policy deleted snapshots: before=%d after=%d", len(before), len(after))
	}
}

// TestRetention_AutoGCRunsAfterPruning verifies that when auto_gc = true the
// orphaned blobs are removed from disk after pruning.
func TestRetention_AutoGCRunsAfterPruning(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	// 3 snapshots — each with a unique file content so distinct objects are stored.
	for i := 1; i <= 3; i++ {
		writeFile(t, projectRoot, "autogc.go", strings.Repeat("y", i*100))
		createMainSnap(t, projectRoot, mainBranchID, "snap")
		if i < 3 {
			time.Sleep(time.Second)
		}
	}

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	beforeObjects := countFiles(t, objectsDir)

	// retention's auto-GC uses gc.Run's default grace period (objects younger
	// than 15m are always kept, since they might belong to a snapshot still
	// being written concurrently). Backdate every object's mtime so this
	// fast-running test simulates objects old enough to actually collect.
	backdateFiles(t, objectsDir, 20*time.Minute)

	cfg := &config.RetentionConfig{
		MaxSnapshotsPerBranch: 1, // keep only the newest
		AutoGC:                true,
	}
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce: %v", err)
	}

	afterObjects := countFiles(t, objectsDir)
	if afterObjects >= beforeObjects {
		t.Errorf("expected fewer objects after auto-GC: before=%d after=%d", beforeObjects, afterObjects)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// countFiles returns the number of regular files under root, recursively.
func countFiles(t *testing.T, root string) int {
	t.Helper()
	var count int
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("countFiles(%q): %v", root, err)
	}
	return count
}

// backdateFiles sets the mtime of every regular file under root to age in
// the past, so tests can simulate objects old enough to survive gc's
// default grace period without an actual sleep.
func backdateFiles(t *testing.T, root string, age time.Duration) {
	t.Helper()
	past := time.Now().Add(-age)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.Chtimes(path, past, past)
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("backdateFiles(%q): %v", root, err)
	}
}
