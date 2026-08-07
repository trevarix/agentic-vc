// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests for the missing-blob bug: creating a branch before any
// main snapshot exists copies files into the workspace and warms the stat
// cache, so the first branch snapshot is a stat-only pass. Materialization
// must therefore store the blobs itself — otherwise the snapshot references
// objects that don't exist, restore cannot reconstruct the files, and diffs
// against it render every modified file as "+all -0".
package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/objstore"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// branchSnapshotFiles returns the file rows of a snapshot.
func branchSnapshotFiles(t *testing.T, projectRoot, snapID string) []*db.File {
	t.Helper()
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	files, err := store.GetSnapshotFiles(snapID)
	if err != nil {
		t.Fatalf("GetSnapshotFiles: %v", err)
	}
	return files
}

func TestBranch_NoMainSnapshot_FirstSnapshotBlobsAreStored(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)
	writeFile(t, projectRoot, "a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, projectRoot, "sub/b.go", "package sub\n\nfunc B() {}\n")

	// Branch before any snapshot exists on main — the copy-materialization path.
	b, err := branch.Create(projectRoot, "feat/no-base", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.BaseSnapshotID != "" {
		t.Fatalf("expected empty base snapshot, got %q", b.BaseSnapshotID)
	}

	// First snapshot on the branch walks the workspace — a stat-only pass
	// over the warm cache written during materialization.
	ws := branch.WorkspacePath(projectRoot, b.Name)
	snap, err := snapshot.Create(projectRoot, "initial workspace state", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("snapshot.Create: %v", err)
	}

	// Every file row must have its blob in the object store.
	files := branchSnapshotFiles(t, projectRoot, snap.ID)
	if len(files) == 0 {
		t.Fatal("first branch snapshot recorded no files")
	}
	for _, f := range files {
		if !objstore.Exists(projectRoot, f.FileHash) {
			t.Errorf("object missing for %s (hash %s)", f.RelativePath, f.FileHash)
		}
	}

	// And restore must be able to reconstruct the content byte-for-byte.
	restoreDir := t.TempDir()
	if _, err := restore.RestoreToDir(projectRoot, snap.ID, restoreDir); err != nil {
		t.Fatalf("RestoreToDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(restoreDir, "a.go"))
	if err != nil {
		t.Fatalf("read restored a.go: %v", err)
	}
	if string(got) != "package a\n\nfunc A() {}\n" {
		t.Errorf("restored a.go content mismatch: %q", got)
	}
}

func TestBranch_NoMainSnapshot_DiffCountsAreExact(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)
	writeFile(t, projectRoot, "main.go", "line1\nline2\nline3\nline4\n")

	b, err := branch.Create(projectRoot, "feat/diff-counts", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	first, err := snapshot.Create(projectRoot, "baseline", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Edit one line in the workspace, then snapshot again.
	writeFile(t, ws, "main.go", "line1\nline2 edited\nline3\nline4\n")
	second, err := snapshot.Create(projectRoot, "after edit", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	result, err := diff.Compare(projectRoot, first.ID, second.ID)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.Files))
	}
	fd := result.Files[0]
	// The bug rendered this as +4 -0 (old content unreadable). A one-line
	// edit must count exactly one added and one removed line.
	if fd.Type != diff.Modified || fd.LinesAdded != 1 || fd.LinesRemoved != 1 {
		t.Errorf("main.go: type=%s +%d -%d, want modified +1 -1", fd.Type, fd.LinesAdded, fd.LinesRemoved)
	}
}

// TestBranch_OversizedFile_CopiedButNotStored verifies that files above the
// snapshot size cap are stream-copied into the workspace without creating a
// blob: snapshots skip such files entirely, so a blob would be an orphan —
// and buffering the file across parallel workers is the RAM spike to avoid.
func TestBranch_OversizedFile_CopiedButNotStored(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)

	// Cap snapshots at 1 MB, then track a 2 MB file and a small file.
	writeFile(t, projectRoot, ".avc/config.toml", "[snapshot]\nmax_file_size_mb = 1\n")
	big := strings.Repeat("B", 2*1024*1024)
	writeFile(t, projectRoot, "big.bin", big)
	writeFile(t, projectRoot, "small.go", "package small\n")

	b, err := branch.Create(projectRoot, "feat/oversized", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// The oversized file must be present in the workspace, byte-for-byte.
	got, err := os.ReadFile(filepath.Join(ws, "big.bin"))
	if err != nil {
		t.Fatalf("read workspace big.bin: %v", err)
	}
	if string(got) != big {
		t.Error("workspace big.bin content mismatch")
	}

	// But no blob may exist for it — it would be an orphan.
	sum := sha256.Sum256([]byte(big))
	if objstore.Exists(projectRoot, hex.EncodeToString(sum[:])) {
		t.Error("oversized file was stored as a blob — snapshots never reference it")
	}

	// The first snapshot on the branch skips it and records the small file.
	snap, err := snapshot.Create(projectRoot, "baseline", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("snapshot.Create: %v", err)
	}
	if len(snap.SkippedLarge) != 1 || snap.SkippedLarge[0] != "big.bin" {
		t.Errorf("SkippedLarge = %v, want [big.bin]", snap.SkippedLarge)
	}
	for _, f := range branchSnapshotFiles(t, projectRoot, snap.ID) {
		if f.RelativePath == "big.bin" {
			t.Error("oversized file recorded in snapshot despite size cap")
		}
		if !objstore.Exists(projectRoot, f.FileHash) {
			t.Errorf("object missing for %s", f.RelativePath)
		}
	}
}

func TestSnapshot_StaleCacheHitWithMissingObject_RestoresBlob(t *testing.T) {
	projectRoot, _ := setupProjectWithMain(t)
	writeFile(t, projectRoot, "keep.go", "package keep\n")

	b, err := branch.Create(projectRoot, "feat/stale-cache", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	first, err := snapshot.Create(projectRoot, "baseline", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Simulate the pre-fix corruption: delete the stored blob while the warm
	// stat cache still points at it.
	files := branchSnapshotFiles(t, projectRoot, first.ID)
	for _, f := range files {
		if err := os.Remove(objstore.Path(projectRoot, f.FileHash)); err != nil {
			t.Fatalf("remove object: %v", err)
		}
	}
	// Sanity: the cache file exists, so the next snapshot would be stat-only.
	if _, err := os.Stat(statcache.WorkspaceCachePath(projectRoot, b.Name)); err != nil {
		t.Fatalf("workspace stat cache missing: %v", err)
	}

	// The next snapshot must detect the missing object and re-store it
	// instead of trusting the cache.
	second, err := snapshot.Create(projectRoot, "self-heal", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	for _, f := range branchSnapshotFiles(t, projectRoot, second.ID) {
		if !objstore.Exists(projectRoot, f.FileHash) {
			t.Errorf("object still missing for %s after cache-hit re-store", f.RelativePath)
		}
	}
}
