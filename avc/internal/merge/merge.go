// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package merge implements three-way merge of an agent branch back to main.
package merge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/trevarix/agentic-vc/avc/internal/statcache"
)

// FileResult holds the three-way merge decision for a single file.
type FileResult struct {
	Path       string
	Decision   string // "clean" | "conflict" | "skip"
	BaseHash   string
	MainHash   string
	BranchHash string
}

// Result is returned by Preview and Merge.
type Result struct {
	MergeID             string
	BranchName          string
	Files               []FileResult
	Conflicts           int
	Clean               int
	Deleted             int // files removed from main because the branch deleted them
	Skipped             int
	PostMergeSnapshotID string // set after a clean merge; empty on conflicts or preview
	AutoSnapshotID      string // set when Merge captured un-snapshotted workspace changes before merging
	WorkspaceDirtyFiles int    // Preview only: files changed in the workspace since its last snapshot (not reflected below)
}

// Preview computes the merge plan without writing any files or recording a
// merge. It never snapshots the workspace (unlike Merge), so
// WorkspaceDirtyFiles reports un-snapshotted workspace changes that this
// preview does not yet reflect.
func Preview(projectRoot, branchName string) (*Result, error) {
	files, _, agentBranch, mainBranch, err := buildPlan(projectRoot, branchName)
	if err != nil {
		return nil, err
	}
	result := summarise("", agentBranch, mainBranch, files)
	result.WorkspaceDirtyFiles = countWorkspaceDirtyFiles(projectRoot, agentBranch)
	return result, nil
}

// Merge performs the three-way merge of branchName into main.
// It auto-snapshots main before writing, records a merge record, and returns the result.
// If there are conflicts the files are written with conflict markers and status is "conflicts".
func Merge(projectRoot, branchName string) (*Result, error) {
	// Phase 0: if the branch's workspace has un-snapshotted changes, capture
	// them first. Otherwise those changes would be silently absent from the
	// merge and then permanently lost when the workspace is removed after a
	// successful merge. Must run before buildPlan so the merge plan reflects
	// the fresh snapshot, not the stale one.
	autoSnapshotID, err := autoSnapshotDirtyWorkspace(projectRoot, branchName)
	if err != nil {
		return nil, err
	}

	// Phase 1: build the plan (opens and closes its own DB connection).
	files, resolvedBaseID, agentBranch, mainBranch, err := buildPlan(projectRoot, branchName)
	if err != nil {
		return nil, err
	}

	// Phase 2: auto-snapshot main before mutating anything.
	mainSnap, err := snapshot.Create(
		projectRoot,
		fmt.Sprintf("pre-merge: before merging branch '%s'", branchName),
		"avc-merge",
		"auto-snapshot created before merge",
		mainBranch.ID,
		"", // main branch always uses project root as source
	)
	if err != nil {
		return nil, fmt.Errorf("pre-merge snapshot failed: %w", err)
	}

	// Phase 3: record the merge row.
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		return nil, err
	}

	headSnap, err := store.GetHeadSnapshot(agentBranch.ID)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("branch '%s' has no snapshots to merge", branchName)
	}

	now := time.Now().Unix()
	mergeID := newID("merge")
	m := &db.Merge{
		ID:             mergeID,
		ProjectID:      proj.ID,
		BranchID:       agentBranch.ID,
		BaseSnapshotID: resolvedBaseID,
		MainSnapshotID: mainSnap.ID,
		HeadSnapshotID: headSnap.ID,
		Status:         "in_progress",
		StartedAt:      now,
	}
	if err := store.InsertMerge(m); err != nil {
		store.Close()
		return nil, fmt.Errorf("insert merge record: %w", err)
	}
	store.Close()

	// Phase 3.5: apply the plan to disk. Any failure here marks the merge
	// "failed" (rather than leaving it stuck "in_progress" forever) so that
	// `avc merge --abort` can find and roll back the attempt.
	hasConflicts, applyErr := applyPlan(projectRoot, files)
	if applyErr != nil {
		markMergeFailed(projectRoot, mergeID)
		return nil, fmt.Errorf(
			"merge apply failed (main may be partially written — run `avc merge --abort` to roll back): %w",
			applyErr,
		)
	}

	// Invalidate stat cache since we wrote to the project root.
	statcache.Invalidate(projectRoot)

	// Phase 4: record merge files and update status.
	store2, err := db.Open(projectRoot)
	if err != nil {
		markMergeFailed(projectRoot, mergeID)
		return nil, err
	}

	dbFiles := make([]*db.MergeFile, len(files))
	for i, f := range files {
		dbFiles[i] = &db.MergeFile{
			ID:           newID("mf"),
			MergeID:      mergeID,
			RelativePath: f.Path,
			Decision:     f.Decision,
			BaseHash:     f.BaseHash,
			MainHash:     f.MainHash,
			BranchHash:   f.BranchHash,
		}
	}
	if err := store2.InsertMergeFiles(dbFiles); err != nil {
		_ = store2.UpdateMergeStatus(mergeID, "failed", time.Now().Unix())
		store2.Close()
		return nil, fmt.Errorf("insert merge files: %w", err)
	}

	finalStatus := "completed"
	if hasConflicts {
		finalStatus = "conflicts"
	}
	if err := store2.UpdateMergeStatus(mergeID, finalStatus, time.Now().Unix()); err != nil {
		store2.Close()
		return nil, fmt.Errorf("update merge status: %w", err)
	}
	store2.Close()

	result := summarise(mergeID, agentBranch, mainBranch, files)
	result.AutoSnapshotID = autoSnapshotID

	// Post-merge auto-snapshot: capture the merged state as the new HEAD on main.
	// Only created on a clean merge — conflicts must be resolved first.
	if !hasConflicts {
		postSnap, postErr := snapshot.Create(
			projectRoot,
			fmt.Sprintf("post-merge: merged branch '%s'", branchName),
			"avc-merge",
			fmt.Sprintf("automatic snapshot after clean merge of '%s'", branchName),
			mainBranch.ID,
			"", // main always uses the project root as source directory
		)
		if postErr != nil {
			// Non-fatal: merge succeeded; snapshot failure is logged but doesn't fail the merge.
			fmt.Fprintf(os.Stderr, "[avc] warning: post-merge snapshot failed: %v\n", postErr)
		} else {
			result.PostMergeSnapshotID = postSnap.ID
		}

		// Mark the agent branch as merged and remove its workspace (non-fatal).
		if store3, err3 := db.Open(projectRoot); err3 == nil {
			_ = store3.SetBranchStatus(agentBranch.ID, "merged")
			store3.Close()
		}
		_ = branch.RemoveWorkspace(projectRoot, agentBranch.Name)
	}

	return result, nil
}

// Abort restores main from the pre-merge auto-snapshot and marks the merge aborted.
func Abort(projectRoot string) error {
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		return err
	}

	// Look up the last merge by project, not by main's branch ID: merges are
	// recorded under the *agent* branch's ID (see InsertMerge above), so a
	// lookup keyed on main's branch ID never matches and abort always fails.
	m, err := store.GetLastMergeForProject(proj.ID)
	if err != nil {
		store.Close()
		return fmt.Errorf("no merge in progress to abort")
	}
	if m.Status != "in_progress" && m.Status != "conflicts" && m.Status != "failed" {
		store.Close()
		return fmt.Errorf("last merge has status '%s' — nothing to abort", m.Status)
	}

	mainSnapID := m.MainSnapshotID
	mergeID := m.ID
	store.Close()

	// Restore main from the pre-merge snapshot.
	if _, err := restore.RestoreToDir(projectRoot, mainSnapID, projectRoot); err != nil {
		return fmt.Errorf("restore pre-merge snapshot: %w", err)
	}

	// Mark aborted.
	store2, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store2.Close()
	return store2.UpdateMergeStatus(mergeID, "aborted", time.Now().Unix())
}

// autoSnapshotDirtyWorkspace snapshots branchName's workspace if it has
// changed since the branch's last snapshot, so those changes are captured
// before the merge plan is built rather than silently dropped and then lost
// when the workspace is removed after a successful merge. Returns the
// auto-snapshot's ID, or "" if the workspace was already clean (or the
// branch is main, which has no workspace).
func autoSnapshotDirtyWorkspace(projectRoot, branchName string) (string, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return "", err
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		return "", err
	}
	agentBranch, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		store.Close()
		return "", fmt.Errorf("branch '%s' not found", branchName)
	}
	headSnap, headErr := store.GetHeadSnapshot(agentBranch.ID)
	store.Close()

	ws := branch.WorkspacePath(projectRoot, agentBranch.Name)
	if ws == "" {
		return "", nil // main has no workspace
	}
	headSnapID := ""
	if headErr == nil {
		headSnapID = headSnap.ID
	}

	dirtySnap, err := snapshot.CreateIfDirty(
		projectRoot, ws, headSnapID,
		fmt.Sprintf("auto: pre-merge workspace state for '%s'", branchName),
		"avc-merge", "un-snapshotted workspace changes captured before merge",
		agentBranch.ID,
	)
	if err != nil {
		return "", fmt.Errorf("workspace has un-snapshotted changes and auto-snapshot failed: %w", err)
	}
	if dirtySnap == nil {
		return "", nil
	}
	return dirtySnap.ID, nil
}

// countWorkspaceDirtyFiles reports how many files in agentBranch's workspace
// differ from its last snapshot. Read-only — never snapshots — so Preview
// can warn about un-snapshotted work without violating its no-side-effects
// contract.
func countWorkspaceDirtyFiles(projectRoot string, agentBranch *db.Branch) int {
	ws := branch.WorkspacePath(projectRoot, agentBranch.Name)
	if ws == "" {
		return 0
	}
	store, err := db.Open(projectRoot)
	if err != nil {
		return 0
	}
	headSnap, headErr := store.GetHeadSnapshot(agentBranch.ID)
	store.Close()
	if headErr != nil {
		return 0
	}
	result, err := diffpkg.CompareWithCurrentDir(projectRoot, ws, headSnap.ID)
	if err != nil {
		return 0
	}
	return len(result.Files)
}

// applyPlan writes clean files, deletes files removed on the branch, and
// writes conflict markers for files that could not be merged cleanly.
func applyPlan(projectRoot string, files []FileResult) (hasConflicts bool, err error) {
	for _, f := range files {
		if f.Decision == "skip" {
			continue
		}
		dest := filepath.Join(projectRoot, filepath.FromSlash(f.Path))

		switch f.Decision {
		case "clean":
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return hasConflicts, fmt.Errorf("mkdir for %s: %w", f.Path, err)
			}
			data, err := restore.ReadObject(projectRoot, f.BranchHash)
			if err != nil {
				return hasConflicts, fmt.Errorf("read branch object for %s: %w", f.Path, err)
			}
			if err := fileutil.WriteFile(dest, data); err != nil {
				return hasConflicts, fmt.Errorf("write %s: %w", f.Path, err)
			}
		case "delete":
			if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
				return hasConflicts, fmt.Errorf("delete %s: %w", f.Path, err)
			}
			removeEmptyParents(projectRoot, dest)
		case "conflict":
			hasConflicts = true
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return hasConflicts, fmt.Errorf("mkdir for %s: %w", f.Path, err)
			}
			if err := writeConflict(projectRoot, dest, f); err != nil {
				return hasConflicts, fmt.Errorf("write conflict for %s: %w", f.Path, err)
			}
		}
	}
	return hasConflicts, nil
}

// removeEmptyParents removes dest's parent directory and any now-empty
// ancestors up to (but not including) projectRoot, so a deletion merge does
// not leave orphaned empty directories behind. Best-effort: any error simply
// leaves the (now harmless, empty) directory in place.
func removeEmptyParents(projectRoot, dest string) {
	dir := filepath.Dir(dest)
	for len(dir) > len(projectRoot) && strings.HasPrefix(dir, projectRoot) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// markMergeFailed sets a merge's status to "failed", best-effort. Called when
// the apply loop errors out after the merge row was already inserted, so
// `avc merge --abort` can find and roll back the attempt instead of it being
// stuck at "in_progress" forever.
func markMergeFailed(projectRoot, mergeID string) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return
	}
	defer store.Close()
	_ = store.UpdateMergeStatus(mergeID, "failed", time.Now().Unix())
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// buildPlan computes the three-way merge file list and returns the resolved base
// snapshot ID (which may differ from agentBranch.BaseSnapshotID when the branch
// was created before any main snapshot existed).
func buildPlan(projectRoot, branchName string) ([]FileResult, string, *db.Branch, *db.Branch, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, "", nil, nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, "", nil, nil, err
	}

	mainBranch, err := store.EnsureMainBranch(proj.ID)
	if err != nil {
		return nil, "", nil, nil, err
	}

	agentBranch, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("branch '%s' not found", branchName)
	}

	headSnap, err := store.GetHeadSnapshot(agentBranch.ID)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("branch '%s' has no snapshots to merge", branchName)
	}

	// Resolve the merge base: use branch.BaseSnapshotID when set (normal case),
	// or fall back to the branch's oldest snapshot when the branch was created
	// before any main snapshot existed.
	baseSnapID := agentBranch.BaseSnapshotID
	if baseSnapID == "" {
		oldest, err := store.GetOldestSnapshot(agentBranch.ID)
		if err != nil {
			return nil, "", nil, nil, fmt.Errorf("branch '%s' has no snapshots to use as merge base", branchName)
		}
		baseSnapID = oldest.ID
	}

	baseFiles, err := store.GetSnapshotFiles(baseSnapID)
	if err != nil {
		return nil, "", nil, nil, err
	}

	// Resolve main state: use HEAD snapshot when available, else treat main as
	// identical to the base (no changes happened on main since branching, so all
	// branch edits are clean merges with no conflicts).
	var mainFiles []*db.File
	if mainHead, err := store.GetHeadSnapshot(mainBranch.ID); err == nil {
		mainFiles, err = store.GetSnapshotFiles(mainHead.ID)
		if err != nil {
			return nil, "", nil, nil, err
		}
	} else {
		mainFiles = baseFiles
	}

	branchFiles, err := store.GetSnapshotFiles(headSnap.ID)
	if err != nil {
		return nil, "", nil, nil, err
	}

	// Build hash maps keyed by relative path.
	base := fileMap(baseFiles)
	main := fileMap(mainFiles)
	branch := fileMap(branchFiles)

	// Collect the union of all paths.
	pathSet := make(map[string]bool)
	for p := range base {
		pathSet[p] = true
	}
	for p := range main {
		pathSet[p] = true
	}
	for p := range branch {
		pathSet[p] = true
	}

	files := make([]FileResult, 0, len(pathSet))
	for path := range pathSet {
		bh := base[path]
		mh := main[path]
		rh := branch[path]

		var decision string
		switch {
		case bh == mh && bh == rh: // identical in all three
			decision = "skip"
		case bh == rh: // branch unchanged relative to base
			decision = "skip"
		case bh == mh && rh == "": // branch deleted a file main left unchanged
			decision = "delete"
		case bh == mh: // only branch changed (added or modified)
			decision = "clean"
		case mh == rh: // both main and branch changed to same thing
			decision = "skip"
		default: // all three differ
			decision = "conflict"
		}

		files = append(files, FileResult{
			Path:       path,
			Decision:   decision,
			BaseHash:   bh,
			MainHash:   mh,
			BranchHash: rh,
		})
	}

	return files, baseSnapID, agentBranch, mainBranch, nil
}

// fileMap converts a slice of File records to a path→hash map.
// A missing path maps to "" (empty hash = file absent).
func fileMap(files []*db.File) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.RelativePath] = f.FileHash
	}
	return m
}

// writeConflict writes a file with diff3-style conflict markers, showing all
// three versions: main (ours), the common base ancestor, and branch (theirs).
//
// Format:
//
//	<<<<<<< main (ours)
//	<main content>
//	||||||| base (common ancestor)
//	<original content before either side changed it>
//	=======
//	<branch content>
//	>>>>>>> branch (theirs)
//
// Showing the base lets the user understand what each side changed from,
// which is the most important context for resolving a conflict manually.
func writeConflict(projectRoot, dest string, f FileResult) error {
	readObject := func(hash string) ([]byte, error) {
		if hash == "" {
			return nil, nil
		}
		return restore.ReadObject(projectRoot, hash)
	}

	mainContent, err := readObject(f.MainHash)
	if err != nil {
		return fmt.Errorf("read main object for conflict: %w", err)
	}
	baseContent, err := readObject(f.BaseHash)
	if err != nil {
		return fmt.Errorf("read base object for conflict: %w", err)
	}
	branchContent, err := readObject(f.BranchHash)
	if err != nil {
		return fmt.Errorf("read branch object for conflict: %w", err)
	}

	// ensureNewline appends a newline if the content is non-empty and does not
	// already end with one, so markers always start on their own line.
	ensureNewline := func(b []byte) []byte {
		if len(b) > 0 && b[len(b)-1] != '\n' {
			return append(b, '\n')
		}
		return b
	}

	// Label empty sides explicitly (a delete-vs-edit conflict) so the reader
	// isn't left guessing whether a blank section means "empty file" or
	// "file deleted on this side".
	mainLabel := "main (ours)"
	if f.MainHash == "" {
		mainLabel = "main (ours) — file deleted on main"
	}
	branchLabel := "branch (theirs)"
	if f.BranchHash == "" {
		branchLabel = "branch (theirs) — file deleted on branch"
	}

	var sb strings.Builder
	sb.WriteString("<<<<<<< " + mainLabel + "\n")
	sb.Write(ensureNewline(mainContent))
	sb.WriteString("||||||| base (common ancestor)\n")
	sb.Write(ensureNewline(baseContent))
	sb.WriteString("=======\n")
	sb.Write(ensureNewline(branchContent))
	sb.WriteString(">>>>>>> " + branchLabel + "\n")

	return fileutil.WriteFile(dest, []byte(sb.String()))
}

// summarise builds a Result from a file plan.
func summarise(mergeID string, agentBranch, _ *db.Branch, files []FileResult) *Result {
	r := &Result{
		MergeID:    mergeID,
		BranchName: agentBranch.Name,
		Files:      files,
	}
	for _, f := range files {
		switch f.Decision {
		case "clean":
			r.Clean++
		case "delete":
			r.Deleted++
		case "conflict":
			r.Conflicts++
		default:
			r.Skipped++
		}
	}
	return r
}

// newID generates a short prefixed unique identifier using crypto/rand.
func newID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}
