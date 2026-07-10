// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests for docs/plans/04-trust-primitives.md.
package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fsck"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/objstore"
	"github.com/trevarix/agentic-vc/avc/internal/oplog"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/trash"
	"github.com/trevarix/agentic-vc/avc/internal/undo"
)

// recordRestoreOp mirrors what every restore surface (CLI/MCP/web) does
// after a successful restore: append the operation to the log so undo works.
func recordRestoreOp(projectRoot, branchID, undoSnapID, restoredID string) error {
	return oplog.Record(projectRoot, branchID, oplog.KindRestore, undoSnapID, "restored snapshot "+restoredID)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ─── A1 · Line-level three-way merge (diff3) ──────────────────────────────────

func TestDiff3_NonOverlappingEditsMergeCleanly(t *testing.T) {
	base := []byte("func A() {\n\told\n}\n\nfunc B() {\n\told\n}\n")
	main := []byte("func A() {\n\tmain-change\n}\n\nfunc B() {\n\told\n}\n")
	branchC := []byte("func A() {\n\told\n}\n\nfunc B() {\n\tbranch-change\n}\n")

	merged, conflicts, ok := merge.Diff3(base, main, branchC)
	if !ok {
		t.Fatal("Diff3 declined (size cap?) — expected a merge attempt")
	}
	if conflicts != 0 {
		t.Fatalf("conflicts = %d, want 0 (edits are in different functions)\nmerged:\n%s", conflicts, merged)
	}
	want := "func A() {\n\tmain-change\n}\n\nfunc B() {\n\tbranch-change\n}\n"
	if string(merged) != want {
		t.Errorf("merged =\n%s\nwant:\n%s", merged, want)
	}
}

func TestDiff3_SameLineDivergentEditsConflict(t *testing.T) {
	base := []byte("shared\nline-two\nshared-end\n")
	main := []byte("shared\nmain-version\nshared-end\n")
	branchC := []byte("shared\nbranch-version\nshared-end\n")

	merged, conflicts, ok := merge.Diff3(base, main, branchC)
	if !ok {
		t.Fatal("Diff3 declined — expected a merge attempt")
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1\nmerged:\n%s", conflicts, merged)
	}
	out := string(merged)
	// The stable regions must survive outside the hunk, and the hunk must
	// carry both sides plus the base, using the standard markers.
	for _, want := range []string{
		"shared\n", "shared-end\n",
		"<<<<<<< main (ours)", "main-version",
		"||||||| base (common ancestor)", "line-two",
		"=======", "branch-version", ">>>>>>> branch (theirs)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged output missing %q:\n%s", want, out)
		}
	}
}

func TestDiff3_IdenticalEditsBothSidesMergeCleanly(t *testing.T) {
	base := []byte("a\nb\nc\n")
	both := []byte("a\nCHANGED\nc\n")

	merged, conflicts, ok := merge.Diff3(base, both, both)
	if !ok || conflicts != 0 {
		t.Fatalf("ok=%v conflicts=%d, want clean merge", ok, conflicts)
	}
	if string(merged) != string(both) {
		t.Errorf("merged = %q, want %q", merged, both)
	}
}

func TestDiff3_WholeFileRewriteConflicts(t *testing.T) {
	base := []byte("original content\n")
	main := []byte("completely different main\n")
	branchC := []byte("completely different branch\n")

	_, conflicts, ok := merge.Diff3(base, main, branchC)
	if !ok {
		t.Fatal("Diff3 declined — expected a merge attempt")
	}
	if conflicts == 0 {
		t.Error("expected at least one conflict for divergent whole-file rewrites")
	}
}

func TestDiff3_PreservesCRLFOnCleanMerge(t *testing.T) {
	base := []byte("a\r\nb\r\nc\r\n")
	main := []byte("a-main\r\nb\r\nc\r\n")
	branchC := []byte("a\r\nb\r\nc-branch\r\n")

	merged, conflicts, ok := merge.Diff3(base, main, branchC)
	if !ok || conflicts != 0 {
		t.Fatalf("ok=%v conflicts=%d, want clean merge", ok, conflicts)
	}
	want := "a-main\r\nb\r\nc-branch\r\n"
	if string(merged) != want {
		t.Errorf("merged = %q, want %q (CRLF line endings must survive byte-for-byte)", merged, want)
	}
}

// TestMerge_Diff3_EndToEnd verifies the headline scenario: main and branch
// edit different regions of the same file, and the merge combines both edits
// instead of declaring a whole-file conflict.
func TestMerge_Diff3_EndToEnd(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	const baseContent = "func A() {\n\ta-body\n}\n\nfunc B() {\n\tb-body\n}\n"
	writeFile(t, projectRoot, "app.go", baseContent)
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/edit-b", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "app.go", "func A() {\n\ta-body\n}\n\nfunc B() {\n\tb-improved\n}\n")
	if _, err := snapshot.Create(projectRoot, "branch edits B", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Main edits function A after the branch was created.
	writeFile(t, projectRoot, "app.go", "func A() {\n\ta-improved\n}\n\nfunc B() {\n\tb-body\n}\n")
	createMainSnap(t, projectRoot, mainBranchID, "main edits A")

	result, err := merge.Merge(projectRoot, "feat/edit-b")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts != 0 {
		t.Fatalf("Conflicts = %d, want 0 (disjoint regions)", result.Conflicts)
	}
	if result.Merged != 1 {
		t.Errorf("Merged = %d, want 1", result.Merged)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	want := "func A() {\n\ta-improved\n}\n\nfunc B() {\n\tb-improved\n}\n"
	if string(data) != want {
		t.Errorf("merged file =\n%s\nwant:\n%s", data, want)
	}
}

// TestMerge_Diff3_HunkConflictEndToEnd verifies that overlapping edits
// produce hunk-level markers while the non-overlapping region still merges.
func TestMerge_Diff3_HunkConflictEndToEnd(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "app.go", "header\nshared-line\nfooter\nextra-A\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/hunk-conflict", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	// Branch: changes shared-line AND extra-A.
	writeFile(t, ws, "app.go", "header\nbranch-line\nfooter\nextra-B\n")
	if _, err := snapshot.Create(projectRoot, "branch edit", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Main: changes only shared-line, differently.
	writeFile(t, projectRoot, "app.go", "header\nmain-line\nfooter\nextra-A\n")
	createMainSnap(t, projectRoot, mainBranchID, "main edit")

	result, err := merge.Merge(projectRoot, "feat/hunk-conflict")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1", result.Conflicts)
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "app.go"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	out := string(data)
	// The overlapping region carries markers…
	if !strings.Contains(out, "<<<<<<< main (ours)") || !strings.Contains(out, "branch-line") {
		t.Errorf("expected hunk markers with both sides, got:\n%s", out)
	}
	// …while the branch-only change outside it merged cleanly, and the file
	// is NOT one giant whole-file conflict (header/footer stay unmarked).
	if !strings.Contains(out, "extra-B\n") {
		t.Errorf("branch-only change outside the conflict hunk should merge, got:\n%s", out)
	}
	if strings.Count(out, "<<<<<<<") != 1 {
		t.Errorf("expected exactly one conflict hunk, got:\n%s", out)
	}
}

// ─── A2 · Protected paths ──────────────────────────────────────────────────────

func setupProtectedProject(t *testing.T, mode string) (projectRoot, mainBranchID string) {
	t.Helper()
	projectRoot, mainBranchID = setupProjectWithMain(t)

	cfg := &config.Config{}
	cfg.Protect.Paths = []string{"secrets/**"}
	cfg.Protect.Mode = mode
	if err := config.Save(projectRoot, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	writeFile(t, projectRoot, "app.go", "v1")
	writeFile(t, projectRoot, "secrets/token.txt", "old-secret")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/touches-secrets", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "secrets/token.txt", "CHANGED-secret")
	if _, err := snapshot.Create(projectRoot, "branch touches secrets", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}
	return projectRoot, mainBranchID
}

func TestProtect_BlockModeRefusesMerge(t *testing.T) {
	projectRoot, _ := setupProtectedProject(t, "block")

	_, err := merge.Merge(projectRoot, "feat/touches-secrets")
	if err == nil {
		t.Fatal("expected the merge to be refused (protected path changed, mode=block)")
	}
	if !strings.Contains(err.Error(), "secrets/token.txt") {
		t.Errorf("refusal should name the protected path, got: %v", err)
	}

	// Main must be untouched.
	data, readErr := os.ReadFile(filepath.Join(projectRoot, "secrets", "token.txt"))
	if readErr != nil {
		t.Fatalf("read secret: %v", readErr)
	}
	if string(data) != "old-secret" {
		t.Errorf("protected file was modified despite the refusal: %q", data)
	}
}

func TestProtect_AllowProtectedOverridesBlock(t *testing.T) {
	projectRoot, _ := setupProtectedProject(t, "block")

	result, err := merge.MergeWithOptions(projectRoot, "feat/touches-secrets", true)
	if err != nil {
		t.Fatalf("merge with --allow-protected: %v", err)
	}
	if len(result.ProtectedChanges) != 1 || result.ProtectedChanges[0] != "secrets/token.txt" {
		t.Errorf("ProtectedChanges = %v, want [secrets/token.txt]", result.ProtectedChanges)
	}

	data, _ := os.ReadFile(filepath.Join(projectRoot, "secrets", "token.txt"))
	if string(data) != "CHANGED-secret" {
		t.Errorf("merge should have applied the change after override, got %q", data)
	}
}

func TestProtect_WarnModeProceedsWithFlag(t *testing.T) {
	projectRoot, _ := setupProtectedProject(t, "warn")

	result, err := merge.Merge(projectRoot, "feat/touches-secrets")
	if err != nil {
		t.Fatalf("merge in warn mode should proceed: %v", err)
	}
	if len(result.ProtectedChanges) != 1 {
		t.Errorf("ProtectedChanges = %v, want the changed secret flagged", result.ProtectedChanges)
	}
	if result.ProtectedMode != "warn" {
		t.Errorf("ProtectedMode = %q, want \"warn\"", result.ProtectedMode)
	}
}

func TestProtect_PreviewReportsWithoutGating(t *testing.T) {
	projectRoot, _ := setupProtectedProject(t, "block")

	result, err := merge.Preview(projectRoot, "feat/touches-secrets")
	if err != nil {
		t.Fatalf("preview must never be gated: %v", err)
	}
	if len(result.ProtectedChanges) != 1 {
		t.Errorf("Preview.ProtectedChanges = %v, want the protected path reported", result.ProtectedChanges)
	}
}

// ─── A3 · Universal undo ───────────────────────────────────────────────────────

func TestUndo_ReversesRestoreAndRedoes(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "app.go", "v1")
	snapV1 := createMainSnap(t, projectRoot, mainBranchID, "v1")
	writeFile(t, projectRoot, "app.go", "v2")
	createMainSnap(t, projectRoot, mainBranchID, "v2")

	// Restore back to v1, the way the CLI does it: safety snapshot, restore, record.
	preSnap, err := snapshot.CreateBeforeRestore(projectRoot, projectRoot, mainBranchID, snapV1.ID)
	if err != nil {
		t.Fatalf("CreateBeforeRestore: %v", err)
	}
	if _, err := restore.RestoreToDir(projectRoot, snapV1.ID, projectRoot); err != nil {
		t.Fatalf("restore: %v", err)
	}
	undoID := ""
	if preSnap != nil {
		undoID = preSnap.ID
	}
	if err := recordRestoreOp(projectRoot, mainBranchID, undoID, snapV1.ID); err != nil {
		t.Fatalf("record op: %v", err)
	}

	if data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go")); string(data) != "v1" {
		t.Fatalf("after restore, app.go = %q, want v1", data)
	}

	// Undo → back to v2.
	result, err := undo.Undo(projectRoot)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if result.UndoneKind != "restore" {
		t.Errorf("UndoneKind = %q, want restore", result.UndoneKind)
	}
	if data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go")); string(data) != "v2" {
		t.Errorf("after undo, app.go = %q, want v2", data)
	}

	// Undo again → redo → back to v1.
	if _, err := undo.Undo(projectRoot); err != nil {
		t.Fatalf("redo (second undo): %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go")); string(data) != "v1" {
		t.Errorf("after redo, app.go = %q, want v1", data)
	}
}

func TestUndo_ReversesMergeAndReactivatesBranch(t *testing.T) {
	projectRoot, branchName := setupMergeBase(t, "feat/undo-merge", "original\n", "branch-version\n")

	if _, err := merge.Merge(projectRoot, branchName); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go")); string(data) != "branch-version\n" {
		t.Fatalf("merge did not apply: %q", data)
	}

	result, err := undo.Undo(projectRoot)
	if err != nil {
		t.Fatalf("undo merge: %v", err)
	}
	if result.UndoneKind != "merge" {
		t.Errorf("UndoneKind = %q, want merge", result.UndoneKind)
	}
	if result.ReactivatedBranch != branchName {
		t.Errorf("ReactivatedBranch = %q, want %q", result.ReactivatedBranch, branchName)
	}

	// Main is back to its pre-merge content.
	if data, _ := os.ReadFile(filepath.Join(projectRoot, "app.go")); string(data) != "original\n" {
		t.Errorf("after undo, main app.go = %q, want original", data)
	}

	// The branch is active again with a rebuilt workspace.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	proj, _ := store.GetProject(projectRoot)
	b, bErr := store.GetBranchByName(proj.ID, branchName)
	store.Close()
	if bErr != nil {
		t.Fatalf("get branch: %v", bErr)
	}
	if b.Status != "active" {
		t.Errorf("branch status = %q, want active", b.Status)
	}
	ws := branch.WorkspacePath(projectRoot, branchName)
	if data, err := os.ReadFile(filepath.Join(ws, "app.go")); err != nil || string(data) != "branch-version\n" {
		t.Errorf("workspace should be rebuilt with branch content, got %q (err=%v)", data, err)
	}
}

func TestTrashRestore_PutsQuarantinedFileBack(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "keep.txt", "keep")
	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	writeFile(t, projectRoot, "work-in-progress.txt", "precious uncommitted work")

	result, err := restore.RestoreToDir(projectRoot, snap.ID, projectRoot)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.TrashOpID == "" {
		t.Fatal("expected the untracked file to be quarantined")
	}

	restored, skipped, err := trash.Restore(projectRoot, result.TrashOpID, "")
	if err != nil {
		t.Fatalf("trash.Restore: %v", err)
	}
	if len(restored) != 1 || restored[0] != "work-in-progress.txt" {
		t.Errorf("restored = %v, want [work-in-progress.txt]", restored)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "work-in-progress.txt"))
	if err != nil || string(data) != "precious uncommitted work" {
		t.Errorf("file not restored correctly: %q (err=%v)", data, err)
	}
}

func TestTrashRestore_NeverOverwritesLiveFiles(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "keep.txt", "keep")
	snap, err := snapshot.Create(projectRoot, "baseline", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	writeFile(t, projectRoot, "contested.txt", "quarantined version")
	result, err := restore.RestoreToDir(projectRoot, snap.ID, projectRoot)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// A new file now lives at the same path.
	writeFile(t, projectRoot, "contested.txt", "newer live version")

	restored, skipped, err := trash.Restore(projectRoot, result.TrashOpID, "")
	if err != nil {
		t.Fatalf("trash.Restore: %v", err)
	}
	if len(restored) != 0 || len(skipped) != 1 {
		t.Errorf("restored=%v skipped=%v, want the live file skipped", restored, skipped)
	}
	data, _ := os.ReadFile(filepath.Join(projectRoot, "contested.txt"))
	if string(data) != "newer live version" {
		t.Errorf("live file was overwritten: %q", data)
	}
}

// ─── A4 · Object store v2 (zstd) + fsck ───────────────────────────────────────

func TestObjstore_CompressibleContentRoundTrips(t *testing.T) {
	projectRoot := setupTestProject(t)
	content := bytes.Repeat([]byte("this line compresses very well indeed\n"), 200)
	hash := sha256Hex(content)

	if err := objstore.Store(projectRoot, hash, content); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// On disk it should be the compressed v2 form (much smaller).
	path := objstore.Path(projectRoot, hash)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat object: %v", err)
	}
	if info.Size() >= int64(len(content)) {
		t.Errorf("on-disk size %d >= raw size %d — expected compression to win", info.Size(), len(content))
	}
	objInfo := objstore.Stat(path, info.Size())
	if !objInfo.Compressed {
		t.Error("Stat should report the object as compressed")
	}
	if objInfo.RawSize != uint64(len(content)) {
		t.Errorf("Stat.RawSize = %d, want %d", objInfo.RawSize, len(content))
	}

	got, err := objstore.Read(projectRoot, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("round-tripped content differs from original")
	}
}

func TestObjstore_LegacyRawObjectStillReadable(t *testing.T) {
	projectRoot := setupTestProject(t)
	content := []byte("a legacy object written before compression existed")
	hash := sha256Hex(content)

	// Simulate a pre-v2 store: raw bytes directly on the final path.
	path := filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write legacy object: %v", err)
	}

	got, err := objstore.Read(projectRoot, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("legacy object misread: %q", got)
	}
}

func TestObjstore_RawContentStartingWithMagicIsNotMisread(t *testing.T) {
	projectRoot := setupTestProject(t)
	// Pathological legacy object whose own content begins with the v2 magic
	// but is not a valid compressed object — must fall back to raw bytes.
	content := append([]byte("AVCO\x01"), bytes.Repeat([]byte("not really a zstd frame"), 10)...)
	hash := sha256Hex(content)

	path := filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := objstore.Read(projectRoot, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("magic-prefixed raw content was misread: got %d bytes, want %d", len(got), len(content))
	}
}

func TestFsck_DetectsAndQuarantinesCorruption(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "app.go", "intact content that will be corrupted on disk")
	snap := createMainSnap(t, projectRoot, mainBranchID, "victim")

	// Intact store first.
	clean, err := fsck.Run(projectRoot, false)
	if err != nil {
		t.Fatalf("fsck (clean): %v", err)
	}
	if len(clean.Corrupt) != 0 {
		t.Fatalf("expected an intact store, got %d corrupt", len(clean.Corrupt))
	}

	// Corrupt the object holding app.go.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	files, err := store.GetSnapshotFiles(snap.ID)
	store.Close()
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	var victimHash string
	for _, f := range files {
		if f.RelativePath == "app.go" {
			victimHash = f.FileHash
		}
	}
	if victimHash == "" {
		t.Fatal("app.go not found in snapshot")
	}
	if err := os.WriteFile(objstore.Path(projectRoot, victimHash), []byte("bitrot"), 0644); err != nil {
		t.Fatalf("corrupt object: %v", err)
	}

	// fsck without repair: detected, mapped to the snapshot, left in place.
	result, err := fsck.Run(projectRoot, false)
	if err != nil {
		t.Fatalf("fsck (corrupt): %v", err)
	}
	if len(result.Corrupt) != 1 || result.Corrupt[0].Hash != victimHash {
		t.Fatalf("Corrupt = %+v, want the corrupted hash flagged", result.Corrupt)
	}
	foundSnap := false
	for _, id := range result.Corrupt[0].AffectedSnapshots {
		if id == snap.ID {
			foundSnap = true
		}
	}
	if !foundSnap {
		t.Errorf("AffectedSnapshots = %v, should include %s", result.Corrupt[0].AffectedSnapshots, snap.ID)
	}

	// fsck --repair: quarantined out of the store.
	repaired, err := fsck.Run(projectRoot, true)
	if err != nil {
		t.Fatalf("fsck --repair: %v", err)
	}
	if len(repaired.Corrupt) != 1 || repaired.Corrupt[0].QuarantinedTo == "" {
		t.Fatalf("expected the corrupt object to be quarantined, got %+v", repaired.Corrupt)
	}
	if _, err := os.Stat(objstore.Path(projectRoot, victimHash)); !os.IsNotExist(err) {
		t.Error("corrupt object should be gone from the store after --repair")
	}
	if _, err := os.Stat(repaired.Corrupt[0].QuarantinedTo); err != nil {
		t.Errorf("quarantined copy should exist at %s: %v", repaired.Corrupt[0].QuarantinedTo, err)
	}
}
