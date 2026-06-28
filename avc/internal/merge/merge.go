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

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/snapshot"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/statcache"
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
	MergeID    string
	BranchName string
	Files      []FileResult
	Conflicts  int
	Clean      int
	Skipped    int
}

// Preview computes the merge plan without writing any files or recording a merge.
func Preview(projectRoot, branchName string) (*Result, error) {
	files, _, agentBranch, mainBranch, err := buildPlan(projectRoot, branchName)
	if err != nil {
		return nil, err
	}
	return summarise("", agentBranch, mainBranch, files), nil
}

// Merge performs the three-way merge of branchName into main.
// It auto-snapshots main before writing, records a merge record, and returns the result.
// If there are conflicts the files are written with conflict markers and status is "conflicts".
func Merge(projectRoot, branchName string) (*Result, error) {
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

	// Phase 3: record the merge row and apply files.
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}

	headSnap, err := store.GetHeadSnapshot(agentBranch.ID)
	if err != nil {
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
		return nil, fmt.Errorf("insert merge record: %w", err)
	}
	store.Close()

	// Apply clean files; write conflict markers for conflicts.
	hasConflicts := false
	for _, f := range files {
		if f.Decision == "skip" {
			continue
		}
		dest := filepath.Join(projectRoot, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return nil, fmt.Errorf("mkdir for %s: %w", f.Path, err)
		}
		if f.Decision == "clean" {
			data, err := restore.ReadObject(projectRoot, f.BranchHash)
			if err != nil {
				return nil, fmt.Errorf("read branch object for %s: %w", f.Path, err)
			}
			if err := fileutil.WriteFile(dest, data); err != nil {
				return nil, fmt.Errorf("write %s: %w", f.Path, err)
			}
		} else {
			// conflict
			hasConflicts = true
			if err := writeConflict(projectRoot, dest, f); err != nil {
				return nil, fmt.Errorf("write conflict for %s: %w", f.Path, err)
			}
		}
	}

	// Invalidate stat cache since we wrote to the project root.
	statcache.Invalidate(projectRoot)

	// Phase 4: record merge files and update status.
	store2, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store2.Close()

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
		return nil, fmt.Errorf("insert merge files: %w", err)
	}

	finalStatus := "completed"
	if hasConflicts {
		finalStatus = "conflicts"
	}
	if err := store2.UpdateMergeStatus(mergeID, finalStatus, time.Now().Unix()); err != nil {
		return nil, fmt.Errorf("update merge status: %w", err)
	}

	result := summarise(mergeID, agentBranch, mainBranch, files)
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

	mainBranch, err := store.EnsureMainBranch(proj.ID)
	if err != nil {
		store.Close()
		return err
	}

	m, err := store.GetLastMerge(mainBranch.ID)
	if err != nil {
		// try any branch
		store.Close()
		return fmt.Errorf("no merge in progress to abort")
	}
	if m.Status != "in_progress" && m.Status != "conflicts" {
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
		case bh == mh: // only branch changed
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

// writeConflict writes a file with conflict markers showing main vs branch content.
func writeConflict(projectRoot, dest string, f FileResult) error {
	var mainContent, branchContent []byte

	if f.MainHash != "" {
		data, err := restore.ReadObject(projectRoot, f.MainHash)
		if err != nil {
			return err
		}
		mainContent = data
	}
	if f.BranchHash != "" {
		data, err := restore.ReadObject(projectRoot, f.BranchHash)
		if err != nil {
			return err
		}
		branchContent = data
	}

	var sb strings.Builder
	sb.WriteString("<<<<<<< main (ours)\n")
	sb.Write(mainContent)
	if len(mainContent) > 0 && mainContent[len(mainContent)-1] != '\n' {
		sb.WriteByte('\n')
	}
	sb.WriteString("=======\n")
	sb.Write(branchContent)
	if len(branchContent) > 0 && branchContent[len(branchContent)-1] != '\n' {
		sb.WriteByte('\n')
	}
	sb.WriteString(">>>>>>> branch (theirs)\n")

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
