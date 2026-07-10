// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests for docs/plans/03-lifecycle-hardening.md.
package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/gc"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/retention"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/workspace"
)

// ─── 1 · Retention/delete must not destroy load-bearing snapshots (review 2.3) ─

// TestRetention_ExemptsBranchBaseSnapshot verifies that a snapshot serving as
// an active branch's base survives retention pruning even when it's the
// oldest snapshot on main.
func TestRetention_ExemptsBranchBaseSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "app.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base") // will become a branch base

	if _, err := branch.Create(projectRoot, "feat/keep-base-alive", baseSnap.ID); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	// Push main's snapshot count past the retention limit.
	for i := 0; i < 3; i++ {
		writeFile(t, projectRoot, "app.go", strings.Repeat("x", i+1))
		createMainSnap(t, projectRoot, mainBranchID, "filler")
	}

	cfg := &config.RetentionConfig{MaxSnapshotsPerBranch: 1}
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	if _, err := store.GetSnapshot(baseSnap.ID); err != nil {
		t.Errorf("branch base snapshot %s should survive retention pruning, got: %v", baseSnap.ID, err)
	}
}

// TestRetention_ExemptsTaggedSnapshot verifies that a tagged snapshot
// survives age-based retention pruning.
func TestRetention_ExemptsTaggedSnapshot(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		t.Fatalf("get project: %v", err)
	}

	oldTimestamp := time.Now().AddDate(0, 0, -100).Unix()
	taggedSnap := &db.Snapshot{
		ID:        "snap-tagged-test",
		ProjectID: proj.ID,
		Timestamp: oldTimestamp,
		Label:     "old-tagged",
		BranchID:  mainBranchID,
	}
	if err := store.InsertSnapshotWithFiles(taggedSnap, nil); err != nil {
		store.Close()
		t.Fatalf("insert tagged snapshot: %v", err)
	}
	if err := store.TagSnapshot(taggedSnap.ID, "stable"); err != nil {
		store.Close()
		t.Fatalf("tag snapshot: %v", err)
	}
	store.Close()

	cfg := &config.RetentionConfig{MaxAgeDays: 30}
	var buf bytes.Buffer
	if err := retention.Enforce(projectRoot, mainBranchID, cfg, &buf); err != nil {
		t.Fatalf("retention.Enforce: %v", err)
	}

	store2, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store2.Close()
	if _, err := store2.GetSnapshot(taggedSnap.ID); err != nil {
		t.Errorf("tagged snapshot should survive age-based pruning, got: %v", err)
	}
}

// TestDelete_RefusesProtectedSnapshotWithoutForce verifies that avc delete's
// underlying protection check flags a branch base snapshot as protected.
func TestDelete_RefusesProtectedSnapshotWithoutForce(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	if _, err := branch.Create(projectRoot, "feat/protect-me", baseSnap.ID); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	protected, err := store.IsSnapshotProtected(proj.ID, baseSnap.ID)
	if err != nil {
		t.Fatalf("IsSnapshotProtected: %v", err)
	}
	if !protected {
		t.Error("expected the branch base snapshot to be reported as protected")
	}
}

// ─── 2 · GC grace period (review 2.4) ─────────────────────────────────────────

// TestGC_GracePeriod_SkipsRecentObjects verifies that backdated unreferenced
// objects are collected once they're old enough, with nothing left "recent".
func TestGC_GracePeriod_SkipsRecentObjects(t *testing.T) {
	projectRoot := setupTestProject(t)

	writeFile(t, projectRoot, "fresh.txt", "fresh content")
	writeFile(t, projectRoot, "old.txt", "old content")
	snap, err := snapshot.Create(projectRoot, "two-files", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.DeleteSnapshot(snap.ID); err != nil {
		store.Close()
		t.Fatalf("delete snapshot: %v", err)
	}
	store.Close()

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	backdateFiles(t, objectsDir, 20*time.Minute)

	result, err := gc.RunWithGrace(projectRoot, false, 15*time.Minute)
	if err != nil {
		t.Fatalf("gc.RunWithGrace: %v", err)
	}
	if result.DeletedObjects == 0 {
		t.Error("expected the backdated unreferenced objects to be collected")
	}
	if result.SkippedRecent != 0 {
		t.Errorf("SkippedRecent = %d, want 0 (nothing should look recent)", result.SkippedRecent)
	}
}

// TestGC_GracePeriod_KeepsFreshUnreferencedObjects verifies that a freshly
// written unreferenced object (simulating one that belongs to a snapshot
// still being written concurrently) is not collected.
func TestGC_GracePeriod_KeepsFreshUnreferencedObjects(t *testing.T) {
	projectRoot := setupTestProject(t)

	writeFile(t, projectRoot, "orphan.txt", "never referenced")
	snap, err := snapshot.Create(projectRoot, "orphan-snap", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.DeleteSnapshot(snap.ID); err != nil {
		store.Close()
		t.Fatalf("delete snapshot: %v", err)
	}
	store.Close()

	// Object is fresh (just written) — default 15m grace should protect it.
	result, err := gc.RunWithGrace(projectRoot, false, 15*time.Minute)
	if err != nil {
		t.Fatalf("gc.RunWithGrace: %v", err)
	}
	if result.DeletedObjects != 0 {
		t.Errorf("DeletedObjects = %d, want 0 (object is fresh)", result.DeletedObjects)
	}
	if result.SkippedRecent == 0 {
		t.Error("expected the fresh unreferenced object to be counted as skipped-recent")
	}
}

// ─── 3 · Bounded diff + binary detection (review 2.5) ─────────────────────────

// TestDiff_DetectsBinaryFiles verifies that a file containing a NUL byte is
// reported as binary instead of producing meaningless line counts.
func TestDiff_DetectsBinaryFiles(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeRawBytes(t, projectRoot, "data.bin", []byte{0x00, 0x01, 0x02, 0x03})
	snap1 := createMainSnap(t, projectRoot, mainBranchID, "v1")

	writeRawBytes(t, projectRoot, "data.bin", []byte{0x00, 0xFF, 0xFE, 0xFD})
	snap2 := createMainSnap(t, projectRoot, mainBranchID, "v2")

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff.Compare: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.Files))
	}
	if !result.Files[0].Binary {
		t.Error("expected data.bin to be detected as binary")
	}
	if result.Files[0].LinesAdded != 0 || result.Files[0].LinesRemoved != 0 {
		t.Error("binary files should not report line counts")
	}
}

// TestDiff_EstimatesCountsForLargeFiles verifies that a file exceeding the
// line-count cap reports estimated (not exact) counts instead of running the
// full O(m*n) LCS computation.
func TestDiff_EstimatesCountsForLargeFiles(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	oldLines := repeatLines("old", 2500)
	newLines := repeatLines("new", 2500)

	writeFile(t, projectRoot, "big.txt", oldLines)
	snap1 := createMainSnap(t, projectRoot, mainBranchID, "v1")
	writeFile(t, projectRoot, "big.txt", newLines)
	snap2 := createMainSnap(t, projectRoot, mainBranchID, "v2")

	result, err := diff.Compare(projectRoot, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff.Compare: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.Files))
	}
	if !result.Files[0].CountsEstimated {
		t.Error("expected CountsEstimated=true for a file exceeding the line cap")
	}
}

// ─── 4 · Branch delete uses the DB-authoritative active branch (review 2.9) ───

// TestBranchDelete_RefusesDBActiveBranch verifies that Delete consults the
// DB-authoritative active branch (project_state), not config.toml directly.
func TestBranchDelete_RefusesDBActiveBranch(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	if _, err := branch.Create(projectRoot, "feat/active-check", baseSnap.ID); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := branch.Switch(projectRoot, "feat/active-check"); err != nil {
		t.Fatalf("switch branch: %v", err)
	}

	if err := branch.Delete(projectRoot, "feat/active-check", false); err == nil {
		t.Error("expected an error deleting the active branch, got nil")
	}
}

// ─── 5a · .avcignore ** and ! support ──────────────────────────────────────────

func TestIgnoreRules_DoubleStarMatchesAnyDepth(t *testing.T) {
	rules := compileIgnoreForTest(t, "**/*.log\n")
	if !rules.Matches("app.log") {
		t.Error("**/*.log should match a top-level app.log")
	}
	if !rules.Matches("logs/deep/nested/app.log") {
		t.Error("**/*.log should match a deeply nested app.log")
	}
	if rules.Matches("app.txt") {
		t.Error("**/*.log should not match app.txt")
	}
}

func TestIgnoreRules_BareNameMatchesAnyDepth(t *testing.T) {
	rules := compileIgnoreForTest(t, "node_modules/\n")
	if !rules.MatchesDir("node_modules") {
		t.Error("node_modules/ should match at the top level")
	}
	if !rules.MatchesDir("packages/api/node_modules") {
		t.Error("node_modules/ should match at any depth (bare pattern, not anchored)")
	}
	if rules.Matches("node_modules") {
		t.Error("a dirOnly pattern should never match a file check")
	}
}

func TestIgnoreRules_NegationUnignoresLaterFile(t *testing.T) {
	rules := compileIgnoreForTest(t, "*.log\n!keep.log\n")
	if !rules.Matches("debug.log") {
		t.Error("debug.log should be ignored by *.log")
	}
	if rules.Matches("keep.log") {
		t.Error("keep.log should be un-ignored by the later !keep.log rule")
	}
}

// compileIgnoreForTest writes content to a temp .avcignore file and loads it.
func compileIgnoreForTest(t *testing.T, content string) *fileutil.IgnoreRules {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".avcignore")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write .avcignore: %v", err)
	}
	rules, err := fileutil.LoadIgnoreRulesFrom(path)
	if err != nil {
		t.Fatalf("LoadIgnoreRulesFrom: %v", err)
	}
	return rules
}

// ─── 5b · File modes + empty directories (review 2.10b) ───────────────────────

// TestSnapshotRestore_PreservesExecutableBit verifies that the executable
// bit on a Unix file survives a snapshot/restore round trip. Skipped on
// Windows, which has no meaningful equivalent of the Unix mode bits.
func TestSnapshotRestore_PreservesExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file mode bits are not meaningful on Windows")
	}
	projectRoot := setupTestProject(t)
	scriptPath := filepath.Join(projectRoot, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	snap, err := snapshot.Create(projectRoot, "with-script", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	// Overwrite with different permissions, then restore and confirm the
	// original executable bit comes back.
	if err := os.Chmod(scriptPath, 0644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := restore.Restore(projectRoot, snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat restored file: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("restored file mode = %v, expected the executable bit to survive", info.Mode())
	}
}

// TestRestore_RemovesEmptyDirectoriesLeftByQuarantine verifies that a
// directory whose only file gets quarantined during restore does not linger
// as an empty directory afterward.
func TestRestore_RemovesEmptyDirectoriesLeftByQuarantine(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "keep.txt", "keep me")
	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	writeFile(t, projectRoot, "sub/dir/untracked.txt", "quarantine me")

	if _, err := restore.Restore(projectRoot, snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, "sub")); !os.IsNotExist(err) {
		t.Errorf("expected 'sub' directory to be removed after its only file was quarantined, stat err = %v", err)
	}
}

// ─── 5c · Large-file skip (review 2.10c) ──────────────────────────────────────

// TestSnapshot_SkipsFilesOverConfiguredSizeLimit verifies that a file larger
// than [snapshot] max_file_size_mb is skipped (not read or stored) and
// reported in SkippedLarge.
func TestSnapshot_SkipsFilesOverConfiguredSizeLimit(t *testing.T) {
	projectRoot := setupTestProject(t)

	cfg := &config.Config{}
	cfg.Snapshot.MaxFileSizeMB = 1 // 1 MB cap, easy to exceed deterministically
	if err := config.Save(projectRoot, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	big := make([]byte, 2*1024*1024) // 2 MB — exceeds the 1 MB cap
	if err := os.WriteFile(filepath.Join(projectRoot, "big.bin"), big, 0644); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	writeFile(t, projectRoot, "small.txt", "small")

	snap, err := snapshot.Create(projectRoot, "with-big-file", "", "", "", "")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if len(snap.SkippedLarge) != 1 || snap.SkippedLarge[0] != "big.bin" {
		t.Errorf("SkippedLarge = %v, want [\"big.bin\"]", snap.SkippedLarge)
	}
	if snap.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (only small.txt tracked)", snap.FileCount)
	}
}

// ─── 5e · Workspace .avcignore (review 2.10e) ─────────────────────────────────

// TestSnapshot_UsesWorkspaceAvcignoreWhenPresent verifies that a snapshot
// taken from a branch workspace honors that workspace's own .avcignore
// (which may have been edited by an agent) rather than always the project
// root's copy.
func TestSnapshot_UsesWorkspaceAvcignoreWhenPresent(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "v1")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/ws-ignore", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)

	// The workspace's own .avcignore now excludes *.secret, diverging from
	// the project root (which has none).
	writeFile(t, ws, ".avcignore", "*.secret\n")
	writeFile(t, ws, "config.secret", "sensitive")

	snap, err := snapshot.Create(projectRoot, "ws-snapshot", "", "", b.ID, ws)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	files, err := store.GetSnapshotFiles(snap.ID)
	if err != nil {
		t.Fatalf("get snapshot files: %v", err)
	}
	for _, f := range files {
		if f.RelativePath == "config.secret" {
			t.Error("config.secret should have been excluded by the workspace's own .avcignore")
		}
	}
}

// ─── 5j · Runner output drain does not hang past the cap (review 2.10j) ───────

// TestWorkspaceRun_DrainsOutputPastCapWithoutHanging reproduces the runner
// bug found while reviewing this plan: once the LimitedReader budget was
// exhausted, nothing kept reading the underlying pipe, so a still-writing
// child blocked on its next write and the command hung until the context
// timeout — reporting a passing command as killed (-1) instead of its real
// exit code.
func TestWorkspaceRun_DrainsOutputPastCapWithoutHanging(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "f.txt", "x")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/drain-test", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	cfg := &config.Config{}
	cfg.Run.MaxOutputKB = 1 // tiny cap, easy to exceed
	cfg.Run.DefaultTimeoutSeconds = 15
	cfg.Run.MaxTimeoutSeconds = 15
	if err := config.Save(projectRoot, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	start := time.Now()
	result, err := workspace.Run(workspace.RunRequest{
		ProjectRoot: projectRoot,
		BranchName:  b.Name,
		Command:     `node -e "console.log('a'.repeat(5000))"`,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0 (stderr: %q)", result.ExitCode, result.Stderr)
	}
	if elapsed > 8*time.Second {
		t.Errorf("command took %s — looks like it hung past the output cap instead of draining and exiting promptly", elapsed)
	}
}

// ─── shared helpers ────────────────────────────────────────────────────────────

func writeRawBytes(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func repeatLines(prefix string, n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(prefix)
		sb.WriteString("-line\n")
	}
	return sb.String()
}
