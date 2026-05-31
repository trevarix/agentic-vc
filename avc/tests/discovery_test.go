// Package tests — Phase 5: Snapshot Discovery & Organisation tests.
//
// Covers:
//   5.1 ListSnapshotsFiltered — search, agent, changed, since/until, limit
//   5.1 avc search alias (CLI flag surface verified via DB method directly)
//   5.2 TagSnapshot / UntagSnapshot / GetSnapshotTags / ListSnapshotsByTag
//   5.3 ClearDiffCache / DiffCacheStats
package tests

import (
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// createSnap is a shorthand for creating a snapshot with specific metadata.
func createSnap(t *testing.T, root, branchID, label, agent, notes string) *snapshot.Result {
	t.Helper()
	snap, err := snapshot.Create(root, label, agent, notes, branchID, "")
	if err != nil {
		t.Fatalf("snapshot.Create(%q): %v", label, err)
	}
	return snap
}

// ─── 5.1 ListSnapshotsFiltered ────────────────────────────────────────────────

func TestListFiltered_SearchLabel(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	createSnap(t, root, mainBranchID, "auth refactor start", "claude", "")
	createSnap(t, root, mainBranchID, "payment bug fix", "claude", "")
	createSnap(t, root, mainBranchID, "auth refactor end", "claude", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{Query: "auth refactor", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'auth refactor', got %d", len(results))
	}
	for _, s := range results {
		if s.Label == "payment bug fix" {
			t.Errorf("payment bug fix should not appear in auth refactor search")
		}
	}
}

func TestListFiltered_SearchNotes(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	createSnap(t, root, mainBranchID, "checkpoint A", "claude", "migrated auth module")
	createSnap(t, root, mainBranchID, "checkpoint B", "claude", "fixed payment logic")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{Query: "migrated auth", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Label != "checkpoint A" {
		t.Errorf("expected 1 result 'checkpoint A', got %v", results)
	}
}

func TestListFiltered_AgentFilter(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	createSnap(t, root, mainBranchID, "by claude", "claude", "")
	createSnap(t, root, mainBranchID, "by human", "human", "")
	createSnap(t, root, mainBranchID, "also claude", "claude", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{AgentName: "claude", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 claude snapshots, got %d", len(results))
	}
	for _, s := range results {
		if s.AgentName != "claude" {
			t.Errorf("expected agent 'claude', got %q", s.AgentName)
		}
	}
}

func TestListFiltered_ChangedFile(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	// Snapshot 1: only auth.go
	writeFile(t, root, "auth.go", "v1")
	snap1 := createSnap(t, root, mainBranchID, "auth snap", "test", "")

	// Snapshot 2: only users.go
	writeFile(t, root, "users.go", "v1")
	createSnap(t, root, mainBranchID, "users snap", "test", "")

	_ = snap1 // referenced for clarity

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{FilePath: "auth.go", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one snapshot containing auth.go")
	}
	found := false
	for _, s := range results {
		if s.Label == "auth snap" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'auth snap' in results for --changed auth.go, got %v", results)
	}
}

func TestListFiltered_SinceUntil(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	// Create a snapshot, sleep 1 second, note the boundary, then create another.
	createSnap(t, root, mainBranchID, "old snap", "test", "")
	time.Sleep(time.Second)
	boundary := time.Now().Unix()
	time.Sleep(time.Second)
	createSnap(t, root, mainBranchID, "new snap", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Since boundary: should return only "new snap".
	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{Since: boundary, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Label != "new snap" {
		t.Errorf("since filter: expected [new snap], got %v", results)
	}

	// Until boundary: should return only "old snap".
	results, err = store.ListSnapshotsFiltered(db.SnapshotFilter{Until: boundary, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Label != "old snap" {
		t.Errorf("until filter: expected [old snap], got %v", results)
	}
}

func TestListFiltered_Limit(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	for i := 0; i < 5; i++ {
		createSnap(t, root, mainBranchID, "snap", "test", "")
	}

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("limit 3: expected 3 results, got %d", len(results))
	}
}

func TestListFiltered_DefaultLimit50(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	// Create 55 snapshots; default limit should return only 50.
	for i := 0; i < 55; i++ {
		createSnap(t, root, mainBranchID, "snap", "test", "")
	}

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{}) // Limit=0 → default 50
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 50 {
		t.Errorf("default limit: expected 50, got %d", len(results))
	}
}

// ─── 5.2 Snapshot tags ────────────────────────────────────────────────────────

func TestTagSnapshot_ApplyAndRetrieve(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	snap := createSnap(t, root, mainBranchID, "release", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.TagSnapshot(snap.ID, "stable"); err != nil {
		t.Fatalf("TagSnapshot: %v", err)
	}

	tags, err := store.GetSnapshotTags(snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0] != "stable" {
		t.Errorf("expected [stable], got %v", tags)
	}
}

func TestTagSnapshot_Idempotent(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	snap := createSnap(t, root, mainBranchID, "release", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Apply the same tag twice — should not error or duplicate.
	store.TagSnapshot(snap.ID, "stable")
	store.TagSnapshot(snap.ID, "stable")

	tags, _ := store.GetSnapshotTags(snap.ID)
	if len(tags) != 1 {
		t.Errorf("expected 1 tag (idempotent), got %d", len(tags))
	}
}

func TestUntagSnapshot_RemovesTag(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	snap := createSnap(t, root, mainBranchID, "release", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.TagSnapshot(snap.ID, "stable")
	store.TagSnapshot(snap.ID, "v1.0")

	if err := store.UntagSnapshot(snap.ID, "stable"); err != nil {
		t.Fatalf("UntagSnapshot: %v", err)
	}

	tags, _ := store.GetSnapshotTags(snap.ID)
	if len(tags) != 1 || tags[0] != "v1.0" {
		t.Errorf("after untag: expected [v1.0], got %v", tags)
	}
}

func TestUntagSnapshot_NoopWhenTagAbsent(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)
	snap := createSnap(t, root, mainBranchID, "release", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Untag a tag that was never applied — should return nil.
	if err := store.UntagSnapshot(snap.ID, "nonexistent"); err != nil {
		t.Errorf("expected nil on untag-nonexistent, got %v", err)
	}
}

func TestListSnapshotsByTag(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	s1 := createSnap(t, root, mainBranchID, "snap1", "test", "")
	s2 := createSnap(t, root, mainBranchID, "snap2", "test", "")
	createSnap(t, root, mainBranchID, "snap3", "test", "") // no tag

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.TagSnapshot(s1.ID, "stable")
	store.TagSnapshot(s2.ID, "stable")

	results, err := store.ListSnapshotsByTag("stable")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 stable snapshots, got %d", len(results))
	}
}

func TestListFiltered_TagFilter(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	tagged := createSnap(t, root, mainBranchID, "tagged snap", "test", "")
	createSnap(t, root, mainBranchID, "plain snap", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.TagSnapshot(tagged.ID, "release")

	results, err := store.ListSnapshotsFiltered(db.SnapshotFilter{Tag: "release", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != tagged.ID {
		t.Errorf("expected only tagged snapshot, got %v", results)
	}
}

// ─── 5.3 Diff cache management ────────────────────────────────────────────────

func TestDiffCacheStats_EmptyCache(t *testing.T) {
	root, _ := setupProjectWithMain(t)

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	count, oldest, err := store.DiffCacheStats()
	if err != nil {
		t.Fatalf("DiffCacheStats: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 cached rows on fresh project, got %d", count)
	}
	if oldest != 0 {
		t.Errorf("expected oldest=0 on empty cache, got %d", oldest)
	}
}

func TestClearDiffCache_TruncatesTable(t *testing.T) {
	root, mainBranchID := setupProjectWithMain(t)

	writeFile(t, root, "a.txt", "hello")
	s1 := createSnap(t, root, mainBranchID, "s1", "test", "")
	writeFile(t, root, "a.txt", "world")
	s2 := createSnap(t, root, mainBranchID, "s2", "test", "")

	store, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Manually insert a cache row.
	if err := store.UpsertDiffCache(&db.DiffCache{
		ID:             "dc-test",
		FromSnapshotID: s1.ID,
		ToSnapshotID:   s2.ID,
		FilePath:       "a.txt",
		DiffType:       "modified",
	}); err != nil {
		t.Fatalf("UpsertDiffCache: %v", err)
	}

	count, _, _ := store.DiffCacheStats()
	if count == 0 {
		t.Fatal("expected at least 1 cached row before clear")
	}

	if err := store.ClearDiffCache(); err != nil {
		t.Fatalf("ClearDiffCache: %v", err)
	}

	count, _, _ = store.DiffCacheStats()
	if count != 0 {
		t.Errorf("expected 0 rows after clear, got %d", count)
	}
}
