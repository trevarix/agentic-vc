// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/annotate"
	"github.com/trevarix/agentic-vc/avc/internal/bisect"
	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
	mergepkg "github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/oplog"
	"github.com/trevarix/agentic-vc/avc/internal/policy"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	undopkg "github.com/trevarix/agentic-vc/avc/internal/undo"
	workspacepkg "github.com/trevarix/agentic-vc/avc/internal/workspace"
)

// dispatchTool executes the named tool with the given arguments and wraps the
// result in the MCP content envelope.
func dispatchTool(projectRoot string, compact bool, name string, args map[string]any) (map[string]any, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf(
			"no AVC project found. Run `avc init` in your project directory first, " +
				"then restart Claude Code so the MCP server picks up the project path.",
		)
	}

	var result any
	var err error

	switch name {
	case "avc_snapshot":
		result, err = toolSnapshot(projectRoot, args)
	case "avc_list":
		result, err = toolList(projectRoot, args)
	case "avc_diff":
		result, err = toolDiff(projectRoot, args)
	case "avc_restore":
		result, err = toolRestore(projectRoot, args)
	case "avc_info":
		result, err = toolInfo(projectRoot, args)
	case "avc_delete":
		result, err = toolDelete(projectRoot, args)
	case "avc_branch_create":
		result, err = toolBranchCreate(projectRoot, args)
	case "avc_branch_list":
		result, err = toolBranchList(projectRoot)
	case "avc_branch_switch":
		result, err = toolBranchSwitch(projectRoot, args)
	case "avc_branch_diff":
		result, err = toolBranchDiff(projectRoot, args)
	case "avc_branch_rename":
		result, err = toolBranchRename(projectRoot, args)
	case "avc_branch_abandon":
		result, err = toolBranchAbandon(projectRoot, args)
	case "avc_branch_prune_merged":
		result, err = toolBranchPruneMerged(projectRoot)
	case "avc_merge_preview":
		result, err = toolMergePreview(projectRoot, args)
	case "avc_merge":
		result, err = toolMerge(projectRoot, args)
	case "avc_merge_train":
		result, err = toolMergeTrain(projectRoot, args)
	case "avc_merge_abort":
		result, err = toolMergeAbort(projectRoot)
	case "avc_run_in_workspace":
		result, err = toolRunInWorkspace(projectRoot, args)
	case "avc_bisect":
		result, err = toolBisect(projectRoot, args)
	case "avc_status":
		result, err = toolStatus(projectRoot)
	case "avc_undo":
		result, err = toolUndo(projectRoot)
	case "avc_restore_file":
		result, err = toolRestoreFile(projectRoot, args)
	case "avc_annotate":
		result, err = toolAnnotate(projectRoot, args)
	case "avc_tag_snapshot":
		result, err = toolTagSnapshot(projectRoot, args)
	case "avc_untag_snapshot":
		result, err = toolUntagSnapshot(projectRoot, args)
	case "avc_list_conflicts":
		result, err = toolListConflicts(projectRoot, args)
	case "avc_resolve_conflict":
		result, err = toolResolveConflict(projectRoot, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	if err != nil {
		return nil, err
	}
	return wrapContent(result, compact)
}

// ─── Snapshot tools ───────────────────────────────────────────────────────────

func toolSnapshot(projectRoot string, args map[string]any) (any, error) {
	label := strArg(args, "label")
	if label == "" {
		return nil, fmt.Errorf("label is required")
	}

	agentName := strArg(args, "agent_name")
	if agentName == "" {
		agentName = "agent"
	}

	branchID, err := branchpkg.GetActiveBranchID(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("could not determine active branch: %w", err)
	}
	branchName := branchpkg.GetActiveBranchName(projectRoot)
	sourceDir := branchpkg.WorkspacePath(projectRoot, branchName) // "" for main

	// Remember the branch HEAD before snapshotting so protected-path changes
	// in this snapshot can be reported (early warning — the hard gate is at
	// merge time).
	prevHeadID := ""
	if store, dbErr := db.Open(projectRoot); dbErr == nil {
		if head, headErr := store.GetHeadSnapshot(branchID); headErr == nil {
			prevHeadID = head.ID
		}
		store.Close()
	}

	snap, err := snapshot.CreateWithOptions(projectRoot, snapshot.Options{
		Label:     label,
		AgentName: agentName,
		Notes:     strArg(args, "notes"),
		BranchID:  branchID,
		SourceDir: sourceDir,
		SessionID: strArg(args, "session_id"),
		Task:      strArg(args, "task"),
	})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id":            snap.ID,
		"label":         snap.Label,
		"timestamp":     snap.Timestamp,
		"agent_name":    snap.AgentName,
		"file_count":    snap.FileCount,
		"total_size":    snap.TotalSize,
		"notes":         snap.Notes,
		"branch_id":     snap.BranchID,
		"session_id":    snap.SessionID,
		"task":          snap.Task,
		"summary":       snap.Summary,
		"skipped_large": snap.SkippedLarge,
		"new_files":     snap.NewFiles,
		"carried_files": snap.CarriedFiles,
		"success":       true,
	}
	if snap.CarriedFiles > 0 {
		out["carried_warning"] = fmt.Sprintf(
			"%d previously-tracked file(s) now match an ignore rule but were kept because they still exist on disk — ignoring does not untrack. Delete the files or use an explicit untrack to stop tracking them.",
			snap.CarriedFiles)
	}
	if protected := protectedChangesBetween(projectRoot, prevHeadID, snap.ID); len(protected) > 0 {
		out["protected_changes"] = protected
		out["protected_warning"] = "This snapshot changes paths listed under [protect] in .avc/config.toml. " +
			"A merge touching them will be refused (or flagged in warn mode) — surface this to the user now rather than at merge time."
	}
	return out, nil
}

// protectedChangesBetween returns the paths whose content differs between
// two snapshots and matches the [protect] globs. Hash-only comparison — no
// object reads or line diffs — so it adds negligible cost to a snapshot.
// Either snapshot ID may be "" (treated as an empty file set).
func protectedChangesBetween(projectRoot, fromID, toID string) []string {
	cfg, _ := config.Load(projectRoot)
	if !policy.Enabled(cfg) {
		return nil
	}
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil
	}
	defer store.Close()

	hashesOf := func(id string) map[string]string {
		m := map[string]string{}
		if id == "" {
			return m
		}
		files, err := store.GetSnapshotFiles(id)
		if err != nil {
			return m
		}
		for _, f := range files {
			m[f.RelativePath] = f.FileHash
		}
		return m
	}
	from := hashesOf(fromID)
	to := hashesOf(toID)

	var changed []string
	for p, h := range to {
		if from[p] != h {
			changed = append(changed, p)
		}
	}
	for p := range from {
		if _, ok := to[p]; !ok {
			changed = append(changed, p)
		}
	}
	matched := policy.Check(cfg, changed)
	sort.Strings(matched)
	return matched
}

func toolList(projectRoot string, args map[string]any) (any, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	f := db.SnapshotFilter{
		Query:    strArg(args, "search"),
		AgentName: strArg(args, "agent"),
		FilePath: strArg(args, "changed"),
		Tag:      strArg(args, "tag"),
		Limit:    intArg(args, "limit"),
	}

	// Default to active branch unless --all or any filter is set.
	isFiltered := f.Query != "" || f.AgentName != "" || f.FilePath != "" || f.Tag != ""
	showAll, _ := args["all"].(bool)
	if !showAll && !isFiltered {
		branchID, branchErr := branchpkg.GetActiveBranchID(projectRoot)
		if branchErr == nil {
			f.BranchID = branchID
		}
	}

	snapshots, err := store.ListSnapshotsFiltered(f)
	if err != nil {
		return nil, err
	}

	type snapJSON struct {
		ID           string `json:"id"`
		Label        string `json:"label"`
		Timestamp    int64  `json:"timestamp"`
		AgentName    string `json:"agent_name"`
		FilesChanged int    `json:"files_changed"`
		TotalSize    int64  `json:"total_size"`
		Notes        string `json:"notes"`
		BranchID     string `json:"branch_id"`
	}
	out := make([]snapJSON, len(snapshots))
	for i, s := range snapshots {
		out[i] = snapJSON{s.ID, s.Label, s.Timestamp, s.AgentName, s.FileCount, s.TotalSize, s.Notes, s.BranchID}
	}
	return out, nil
}

func toolDiff(projectRoot string, args map[string]any) (any, error) {
	fromID := strArg(args, "from_id")
	toID := strArg(args, "to_id")
	if fromID == "" || toID == "" {
		return nil, fmt.Errorf("from_id and to_id are required")
	}

	result, err := diffpkg.Compare(projectRoot, fromID, toID)
	if err != nil {
		return nil, err
	}

	type fileDiffJSON struct {
		Path            string `json:"path"`
		Type            string `json:"type"`
		OldHash         string `json:"old_hash,omitempty"`
		NewHash         string `json:"new_hash,omitempty"`
		LinesAdded      int    `json:"lines_added"`
		LinesRemoved    int    `json:"lines_removed"`
		DiffPreview     string `json:"diff_preview,omitempty"`
		Binary          bool   `json:"binary,omitempty"`
		CountsEstimated bool   `json:"counts_estimated,omitempty"`
	}
	files := make([]fileDiffJSON, len(result.Files))
	for i, f := range result.Files {
		files[i] = fileDiffJSON{
			Path:            f.Path,
			Type:            string(f.Type),
			OldHash:         f.OldHash,
			NewHash:         f.NewHash,
			LinesAdded:      f.LinesAdded,
			LinesRemoved:    f.LinesRemoved,
			DiffPreview:     f.DiffPreview,
			Binary:          f.Binary,
			CountsEstimated: f.CountsEstimated,
		}
	}
	return map[string]any{
		"from_snapshot": fromID,
		"to_snapshot":   toID,
		"files":         files,
	}, nil
}

func toolRestore(projectRoot string, args map[string]any) (any, error) {
	id := strArg(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	// Restore into the workspace on non-main branches; project root on main.
	targetDir := projectRoot
	branchName := branchpkg.GetActiveBranchName(projectRoot)
	if ws := branchpkg.WorkspacePath(projectRoot, branchName); ws != "" {
		targetDir = ws
	}

	activeBranchID, err := branchpkg.GetActiveBranchID(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("could not determine active branch: %w", err)
	}

	// Safety net: capture un-snapshotted changes before they are overwritten.
	// A failure here aborts the restore rather than risking silent data loss.
	preSnap, err := snapshot.CreateBeforeRestore(projectRoot, targetDir, activeBranchID, id)
	if err != nil {
		return nil, fmt.Errorf("pre-restore safety snapshot failed (restore aborted to avoid data loss): %w", err)
	}

	result, err := restore.RestoreToDir(projectRoot, id, targetDir)
	if err != nil {
		return nil, err
	}

	undoID := ""
	if preSnap != nil {
		undoID = preSnap.ID
	}

	// Record in the operations log so avc_undo can reverse this restore.
	// Best-effort: the restore already succeeded.
	_ = oplog.Record(projectRoot, activeBranchID, oplog.KindRestore, undoID,
		fmt.Sprintf("restored snapshot %s", id))

	return map[string]any{
		"id":                result.SnapshotID,
		"restored_files":    result.RestoredFiles,
		"restored_size":     result.RestoredSize,
		"quarantined_files": result.QuarantinedFiles,
		"trash_op_id":       result.TrashOpID,
		"undo_snapshot_id":  undoID,
		"target_dir":        targetDir,
		"success":           true,
	}, nil
}

func toolInfo(projectRoot string, args map[string]any) (any, error) {
	id := strArg(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	snap, err := store.GetSnapshot(id)
	if err != nil {
		return nil, err
	}
	files, err := store.GetSnapshotFiles(id)
	if err != nil {
		return nil, err
	}

	type fileJSON struct {
		Path string `json:"path"`
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	}
	fileList := make([]fileJSON, len(files))
	for i, f := range files {
		fileList[i] = fileJSON{f.RelativePath, f.FileHash, f.FileSize}
	}
	return map[string]any{
		"id":         snap.ID,
		"label":      snap.Label,
		"timestamp":  snap.Timestamp,
		"agent_name": snap.AgentName,
		"notes":      snap.Notes,
		"file_count": snap.FileCount,
		"total_size": snap.TotalSize,
		"branch_id":  snap.BranchID,
		"files":      fileList,
	}, nil
}

func toolDelete(projectRoot string, args map[string]any) (any, error) {
	id := strArg(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(id); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", id)
	}

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, err
	}
	// Unlike the CLI, MCP has no --force override: an agent must never be
	// able to delete a branch base, a tagged snapshot, or part of the last
	// merge record on its own judgment.
	protected, err := store.IsSnapshotProtected(proj.ID, id)
	if err != nil {
		return nil, err
	}
	if protected {
		return nil, fmt.Errorf(
			"snapshot '%s' is protected (a branch base, tagged, or part of the last merge record) and cannot be deleted via this tool; "+
				"ask the user to run `avc delete %s --force` if this is intentional", id, id,
		)
	}

	if err := store.DeleteSnapshot(id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "success": true}, nil
}

// ─── Branch tools ─────────────────────────────────────────────────────────────

func toolBranchCreate(projectRoot string, args map[string]any) (any, error) {
	name := strArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := branchpkg.ValidateBranchName(name); err != nil {
		return nil, err
	}

	fromSnapshotID := strArg(args, "from_snapshot_id")
	fromBranch := strArg(args, "from_branch")
	createBranch := func() (*db.Branch, error) {
		if fromBranch != "" {
			return branchpkg.CreateFromBranch(projectRoot, name, fromBranch)
		}
		return branchpkg.Create(projectRoot, name, fromSnapshotID)
	}
	b, err := createBranch()
	if err != nil {
		// On a freshly-initialised project main has no snapshots yet. Take one
		// automatically and retry rather than surfacing a confusing error.
		if strings.Contains(err.Error(), "no snapshots to branch from") {
			// Resolve the main branch ID so the snapshot is scoped correctly.
			// GetHeadSnapshot filters by branch_id, so an unscoped snapshot
			// (branchID="") would be invisible to the next Create attempt.
			var mainID string
			store, dbErr := db.Open(projectRoot)
			if dbErr == nil {
				proj, projErr := store.GetProject(projectRoot)
				if projErr == nil {
					if mb, mbErr := store.GetBranchByName(proj.ID, "main"); mbErr == nil {
						mainID = mb.ID
					}
				}
				store.Close()
			}
			if _, snapErr := snapshot.Create(projectRoot, "auto: initial project state", "agent", "baseline snapshot before first branch", mainID, ""); snapErr != nil {
				return nil, fmt.Errorf("project has no snapshots and auto-snapshot failed: %w", snapErr)
			}
			b, err = createBranch()
		}
		if err != nil {
			return nil, err
		}
	}

	// Auto-switch — creating a branch means you're about to work on it.
	if err := branchpkg.Switch(projectRoot, name); err != nil {
		return nil, fmt.Errorf("branch created but auto-switch failed: %w", err)
	}

	workspacePath := branchpkg.WorkspacePath(projectRoot, b.Name)
	return map[string]any{
		"id":               b.ID,
		"name":             b.Name,
		"base_snapshot_id": b.BaseSnapshotID,
		"workspace":        workspacePath,
		"active":           true,
		"success":          true,
		"instruction": fmt.Sprintf(
			"Branch '%s' is now active. Your workspace is at: %s\n"+
				"Follow these steps exactly:\n"+
				"1. Call avc_snapshot to capture the initial workspace state.\n"+
				"2. Always Read files from the workspace path before editing — never assume workspace content matches what you previously read from the project root.\n"+
				"3. Make your changes inside the workspace path only. NEVER read or write files under the original project root (%s).\n"+
				"4. When the task is complete, call avc_branch_diff and show the full output to the user before offering to merge.",
			b.Name, workspacePath, projectRoot,
		),
	}, nil
}

func toolBranchList(projectRoot string) (any, error) {
	branches, err := branchpkg.List(projectRoot)
	if err != nil {
		return nil, err
	}
	activeName := branchpkg.GetActiveBranchName(projectRoot)

	type branchJSON struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		BaseSnapshotID string `json:"base_snapshot_id"`
		Workspace      string `json:"workspace"`
		Active         bool   `json:"active"`
	}
	out := make([]branchJSON, len(branches))
	for i, b := range branches {
		out[i] = branchJSON{
			ID:             b.ID,
			Name:           b.Name,
			BaseSnapshotID: b.BaseSnapshotID,
			Workspace:      branchpkg.WorkspacePath(projectRoot, b.Name),
			Active:         b.Name == activeName,
		}
	}
	return out, nil
}

func toolBranchSwitch(projectRoot string, args map[string]any) (any, error) {
	name := strArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := branchpkg.Switch(projectRoot, name); err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "success": true}, nil
}

func toolBranchDiff(projectRoot string, args map[string]any) (any, error) {
	name := strArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Cross-branch mode: HEAD of `name` vs HEAD of `against`.
	if against := strArg(args, "against"); against != "" {
		return toolCrossBranchDiff(projectRoot, name, against)
	}

	branches, err := branchpkg.ListByStatus(projectRoot, "")
	if err != nil {
		return nil, err
	}

	var branchID, baseSnapshotID string
	for _, b := range branches {
		if b.Name == name {
			branchID = b.ID
			baseSnapshotID = b.BaseSnapshotID
			break
		}
	}
	if branchID == "" {
		return nil, fmt.Errorf("branch '%s' not found", name)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	// When no base snapshot exists (branch created before any main snapshot),
	// fall back to the oldest snapshot on the branch as the diff base.
	if baseSnapshotID == "" {
		oldest, err := store.GetOldestSnapshot(branchID)
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("branch '%s' has no snapshots yet", name)
		}
		baseSnapshotID = oldest.ID
	}
	head, headErr := store.GetHeadSnapshot(branchID)
	store.Close()
	if headErr != nil {
		return nil, fmt.Errorf("branch '%s' has no snapshots yet", name)
	}

	// stat mode: per-file counts only, no unified-diff previews — keeps the
	// output small on a large branch (a full diff can be multiple MB).
	stat, _ := args["stat"].(bool)

	var result *diffpkg.Result
	if stat {
		result, err = diffpkg.CompareCounts(projectRoot, baseSnapshotID, head.ID)
	} else {
		result, err = diffpkg.Compare(projectRoot, baseSnapshotID, head.ID)
	}
	if err != nil {
		return nil, err
	}

	text := formatBranchDiff(name, baseSnapshotID, head.ID, result, stat)

	// Early warning: flag protected-path changes now, before a merge attempt
	// gets refused by the [protect] gate.
	cfg, _ := config.Load(projectRoot)
	if policy.Enabled(cfg) {
		var paths []string
		for _, f := range result.Files {
			paths = append(paths, f.Path)
		}
		if protected := policy.Check(cfg, paths); len(protected) > 0 {
			sort.Strings(protected)
			consequence := "a merge will be refused unless a human overrides with --allow-protected"
			if policy.Mode(cfg) == policy.ModeWarn {
				consequence = "a merge will be flagged with a warning"
			}
			text = fmt.Sprintf(
				"⚠ PROTECTED PATHS CHANGED: this branch changes %d path(s) listed under [protect] in .avc/config.toml (%s) — %s. Tell the user before offering to merge.\n\n",
				len(protected), strings.Join(protected, ", "), consequence,
			) + text
		}
	}

	return text, nil
}

// toolCrossBranchDiff compares the HEAD snapshots of two branches — how two
// parallel lines of work differ, rather than what one changed since its base.
func toolCrossBranchDiff(projectRoot, name, against string) (any, error) {
	headOf := func(branchName string) (string, error) {
		store, err := db.Open(projectRoot)
		if err != nil {
			return "", err
		}
		defer store.Close()
		proj, err := store.GetProject(projectRoot)
		if err != nil {
			return "", err
		}
		b, err := store.GetBranchByName(proj.ID, branchName)
		if err != nil {
			return "", fmt.Errorf("branch '%s' not found", branchName)
		}
		head, err := store.GetHeadSnapshot(b.ID)
		if err != nil {
			return "", fmt.Errorf("branch '%s' has no snapshots yet", branchName)
		}
		return head.ID, nil
	}

	fromHead, err := headOf(name)
	if err != nil {
		return nil, err
	}
	toHead, err := headOf(against)
	if err != nil {
		return nil, err
	}
	result, err := diffpkg.Compare(projectRoot, fromHead, toHead)
	if err != nil {
		return nil, err
	}
	return formatBranchDiff(fmt.Sprintf("%s → %s", name, against), fromHead, toHead, result, false), nil
}

// formatBranchDiff renders a branch diff as human-readable markdown text.
// In stat mode it emits one line per file (path, type, counts) and no unified
// diff previews — a compact summary suited to agent review of a large branch.
func formatBranchDiff(branch, fromSnap, toSnap string, result *diffpkg.Result, stat bool) string {
	var b strings.Builder

	// Header
	totalAdded, totalRemoved := 0, 0
	for _, f := range result.Files {
		totalAdded += f.LinesAdded
		totalRemoved += f.LinesRemoved
	}
	fileWord := "file"
	if len(result.Files) != 1 {
		fileWord = "files"
	}
	fmt.Fprintf(&b, "Branch: %s\n", branch)
	fmt.Fprintf(&b, "Snapshots: %s → %s\n", fromSnap, toSnap)
	fmt.Fprintf(&b, "%d %s changed  (+%d lines, -%d lines)\n",
		len(result.Files), fileWord, totalAdded, totalRemoved)

	if stat {
		// One compact line per file: type, counts, path.
		for _, f := range result.Files {
			fmt.Fprintf(&b, "  %-8s +%-5d -%-5d  %s\n",
				string(f.Type), f.LinesAdded, f.LinesRemoved, f.Path)
		}
		return b.String()
	}

	// Per-file sections with previews.
	for _, f := range result.Files {
		b.WriteString("\n")
		fmt.Fprintf(&b, "── %s  [%s]  +%d -%d ──\n",
			f.Path, string(f.Type), f.LinesAdded, f.LinesRemoved)
		if f.DiffPreview != "" {
			b.WriteString(f.DiffPreview)
			if f.DiffPreview[len(f.DiffPreview)-1] != '\n' {
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func toolBranchRename(projectRoot string, args map[string]any) (any, error) {
	oldName := strArg(args, "old_name")
	newName := strArg(args, "new_name")
	if oldName == "" || newName == "" {
		return nil, fmt.Errorf("old_name and new_name are required")
	}
	if err := branchpkg.Rename(projectRoot, oldName, newName); err != nil {
		return nil, err
	}
	return map[string]any{
		"old_name": oldName,
		"new_name": newName,
		"success":  true,
	}, nil
}

func toolBranchAbandon(projectRoot string, args map[string]any) (any, error) {
	name := strArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := branchpkg.Abandon(projectRoot, name); err != nil {
		return nil, err
	}
	return map[string]any{
		"name":    name,
		"status":  "abandoned",
		"success": true,
	}, nil
}

func toolBranchPruneMerged(projectRoot string) (any, error) {
	pruned, err := branchpkg.PruneMergedWorkspaces(projectRoot)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"pruned":  pruned,
		"count":   len(pruned),
		"success": true,
	}, nil
}

// ─── Merge tools ──────────────────────────────────────────────────────────────

func mergeResultToMap(result *mergepkg.Result, preview bool) map[string]any {
	type fileJSON struct {
		Path     string `json:"path"`
		Decision string `json:"decision"`
	}
	// Only include files that require attention (clean, delete, or conflict).
	// Skipped files (unchanged since branch base) are omitted — listing all
	// 200+ unchanged files adds noise without useful information.
	var files []fileJSON
	for _, f := range result.Files {
		if f.Decision != "skip" {
			files = append(files, fileJSON{f.Path, f.Decision})
		}
	}
	if files == nil {
		files = []fileJSON{}
	}
	m := map[string]any{
		"merge_id":  result.MergeID,
		"branch":    result.BranchName,
		"preview":   preview,
		"clean":     result.Clean,
		"merged":    result.Merged,
		"deleted":   result.Deleted,
		"conflicts": result.Conflicts,
		"skipped":   result.Skipped,
		"files":     files,
	}
	if result.PostMergeSnapshotID != "" {
		m["post_merge_snapshot_id"] = result.PostMergeSnapshotID
	}
	if result.AutoSnapshotID != "" {
		m["auto_snapshot_id"] = result.AutoSnapshotID
	}
	if preview && result.WorkspaceDirtyFiles > 0 {
		m["workspace_dirty_files"] = result.WorkspaceDirtyFiles
		m["warning"] = fmt.Sprintf(
			"%d file(s) in the workspace have changed since the last snapshot on this branch and are NOT reflected in this preview. Call avc_snapshot first if you want them included.",
			result.WorkspaceDirtyFiles,
		)
	}
	if len(result.ProtectedChanges) > 0 {
		m["protected_changes"] = result.ProtectedChanges
		m["protected_mode"] = result.ProtectedMode
		if result.ProtectedMode == "block" {
			m["protected_warning"] = "This merge changes protected paths ([protect] in .avc/config.toml) and will be refused. " +
				"Only a human can override, by running `avc merge --allow-protected` from the CLI. " +
				"Tell the user which paths are affected and let them decide."
		}
	}
	return m
}

func toolMergePreview(projectRoot string, args map[string]any) (any, error) {
	branch := strArg(args, "branch")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	result, err := mergepkg.Preview(projectRoot, branch)
	if err != nil {
		return nil, err
	}
	return mergeResultToMap(result, true), nil
}

func toolMerge(projectRoot string, args map[string]any) (any, error) {
	branch := strArg(args, "branch")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	// Check for conflicts before writing anything. If conflicts exist, return
	// them and let the agent/user decide how to resolve before retrying.
	plan, err := mergepkg.Preview(projectRoot, branch)
	if err != nil {
		return nil, err
	}
	if plan.Conflicts > 0 {
		m := mergeResultToMap(plan, true)
		m["error"] = fmt.Sprintf(
			"%d conflict(s) detected — merge aborted. Resolve the conflicts listed in 'files' then retry, or call avc_merge_abort to abandon.",
			plan.Conflicts,
		)
		return m, nil
	}

	result, err := mergepkg.Merge(projectRoot, branch)
	if err != nil {
		return nil, err
	}
	return mergeResultToMap(result, false), nil
}

func toolMergeTrain(projectRoot string, args map[string]any) (any, error) {
	raw := strArg(args, "branches")
	if raw == "" {
		return nil, fmt.Errorf("branches is required (comma-separated, in merge order)")
	}
	var branches []string
	for _, b := range strings.Split(raw, ",") {
		if b = strings.TrimSpace(b); b != "" {
			branches = append(branches, b)
		}
	}
	// allowProtected is always false here — the [protect] override stays
	// CLI-only, exactly as it is for avc_merge.
	result, err := mergepkg.Train(projectRoot, branches, strArg(args, "validate"), false)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"results":    result.Results,
		"completed":  result.Completed,
		"stopped_at": result.StoppedAt,
		"success":    result.StoppedAt == "",
	}, nil
}

func toolMergeAbort(projectRoot string) (any, error) {
	if err := mergepkg.Abort(projectRoot); err != nil {
		return nil, err
	}
	return map[string]any{"aborted": true, "success": true}, nil
}

// ─── Workspace run tool ───────────────────────────────────────────────────────

func toolRunInWorkspace(projectRoot string, args map[string]any) (any, error) {
	// Mechanical approval gate: the tool is disabled by default. A human must
	// set [run] enabled = true in .avc/config.toml to permit execution.
	// Agents cannot enable this themselves.
	cfg, _ := config.Load(projectRoot)
	if cfg == nil || !cfg.Run.Enabled {
		return nil, fmt.Errorf(
			"avc_run_in_workspace is disabled. " +
				"To enable it, set `enabled = true` under [run] in .avc/config.toml. " +
				"This must be done manually by a human — agents cannot enable it.",
		)
	}

	branch := strArg(args, "branch")
	if branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	command := strArg(args, "command")
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	result, err := workspacepkg.Run(workspacepkg.RunRequest{
		ProjectRoot:    projectRoot,
		BranchName:     branch,
		Command:        command,
		TimeoutSeconds: intArg(args, "timeout_seconds"),
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"exit_code":      result.ExitCode,
		"stdout":         result.Stdout,
		"stderr":         result.Stderr,
		"workspace_path": result.WorkspacePath,
		"env_info": map[string]any{
			"type":        result.EnvInfo.Type,
			"path":        result.EnvInfo.Path,
			"module_name": result.EnvInfo.ModuleName,
		},
		"sandbox_info": map[string]any{
			"platform": result.SandboxInfo.Platform,
			"layers": map[string]any{
				"env_scrubbing":     result.SandboxInfo.EnvScrubbing,
				"execution_limits":  result.SandboxInfo.ExecutionLimits,
				"process_tree_kill": result.SandboxInfo.ProcessTreeKill,
			},
		},
	}, nil
}

// ─── Bisect tool ──────────────────────────────────────────────────────────────

func toolBisect(projectRoot string, args map[string]any) (any, error) {
	// bisect.Run enforces the [run] enabled gate itself (it executes the
	// command through the same sandbox as toolRunInWorkspace).
	command := strArg(args, "cmd")
	if command == "" {
		return nil, fmt.Errorf("cmd is required")
	}
	good := strArg(args, "good")
	if good == "" {
		return nil, fmt.Errorf("good is required")
	}

	var steps []map[string]any
	result, err := bisect.Run(projectRoot, bisect.Options{
		BranchName:     strArg(args, "branch"),
		GoodID:         good,
		BadID:          strArg(args, "bad"),
		Command:        command,
		TimeoutSeconds: intArg(args, "timeout_seconds"),
		OnStep: func(s bisect.Step) {
			steps = append(steps, map[string]any{
				"snapshot_id": s.SnapshotID,
				"label":       s.Label,
				"exit_code":   s.ExitCode,
				"verdict":     s.Verdict,
			})
		},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"first_bad_id":    result.FirstBadID,
		"first_bad_label": result.FirstBadLabel,
		"predecessor_id":  result.PredecessorID,
		"steps":           result.Steps,
		"step_log":        steps,
		"skipped":         result.Skipped,
		"summary":         result.Summary,
		"ambiguous":       result.Ambiguous,
		"message":         result.Message,
		"success":         true,
	}, nil
}

// ─── Status tool ──────────────────────────────────────────────────────────────

func toolStatus(projectRoot string) (any, error) {
	branchName := branchpkg.GetActiveBranchName(projectRoot)
	sourceDir := branchpkg.WorkspacePath(projectRoot, branchName) // "" for main
	if sourceDir == "" {
		sourceDir = projectRoot
	}

	branchID, err := branchpkg.GetActiveBranchID(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("could not determine active branch: %w", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	head, headErr := store.GetHeadSnapshot(branchID)
	store.Close()

	if headErr != nil {
		return map[string]any{
			"branch":        branchName,
			"snapshot_id":   "",
			"snapshot_label": "",
			"files":         []any{},
			"changed_count": 0,
			"message":       "No snapshots yet. Run avc_snapshot to start tracking.",
		}, nil
	}

	result, err := diffpkg.CompareWithCurrentDir(projectRoot, sourceDir, head.ID)
	if err != nil {
		return nil, err
	}

	type fileDiffJSON struct {
		Path            string `json:"path"`
		Type            string `json:"type"`
		LinesAdded      int    `json:"lines_added"`
		LinesRemoved    int    `json:"lines_removed"`
		Binary          bool   `json:"binary,omitempty"`
		CountsEstimated bool   `json:"counts_estimated,omitempty"`
	}
	files := make([]fileDiffJSON, len(result.Files))
	for i, f := range result.Files {
		files[i] = fileDiffJSON{
			Path:            f.Path,
			Type:            string(f.Type),
			LinesAdded:      f.LinesAdded,
			LinesRemoved:    f.LinesRemoved,
			Binary:          f.Binary,
			CountsEstimated: f.CountsEstimated,
		}
	}
	out := map[string]any{
		"branch":         branchName,
		"snapshot_id":    head.ID,
		"snapshot_label": head.Label,
		"files":          files,
		"changed_count":  len(result.Files),
	}
	cfg, _ := config.Load(projectRoot)
	if policy.Enabled(cfg) {
		var paths []string
		for _, f := range result.Files {
			paths = append(paths, f.Path)
		}
		if protected := policy.Check(cfg, paths); len(protected) > 0 {
			sort.Strings(protected)
			out["protected_changes"] = protected
		}
	}
	return out, nil
}

// ─── Undo tool ────────────────────────────────────────────────────────────────

func toolUndo(projectRoot string) (any, error) {
	result, err := undopkg.Undo(projectRoot)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"undone_kind":          result.UndoneKind,
		"undone_details":       result.UndoneDetails,
		"restored_snapshot_id": result.RestoredSnapshotID,
		"redo_snapshot_id":     result.RedoSnapshotID,
		"branch":               result.BranchName,
		"reactivated_branch":   result.ReactivatedBranch,
		"success":              true,
	}, nil
}

// ─── Restore-file tool ────────────────────────────────────────────────────────

func toolRestoreFile(projectRoot string, args map[string]any) (any, error) {
	snapID := strArg(args, "snapshot_id")
	path := strArg(args, "path")
	if snapID == "" || path == "" {
		return nil, fmt.Errorf("snapshot_id and path are required")
	}

	// Write to workspace on non-main branches; project root on main.
	targetDir := projectRoot
	branchName := branchpkg.GetActiveBranchName(projectRoot)
	if ws := branchpkg.WorkspacePath(projectRoot, branchName); ws != "" {
		targetDir = ws
	}

	result, err := restore.RestoreFileToDir(projectRoot, snapID, path, targetDir)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"snapshot_id": result.SnapshotID,
		"file_path":   result.FilePath,
		"size":        result.Size,
		"target_dir":  targetDir,
		"success":     true,
	}, nil
}

// ─── Annotate tool ────────────────────────────────────────────────────────────

func toolAnnotate(projectRoot string, args map[string]any) (any, error) {
	path := strArg(args, "path")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	result, err := annotate.Annotate(projectRoot, path)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ─── Tag tools ────────────────────────────────────────────────────────────────

func toolTagSnapshot(projectRoot string, args map[string]any) (any, error) {
	snapID := strArg(args, "snapshot_id")
	tag := strArg(args, "tag")
	if snapID == "" || tag == "" {
		return nil, fmt.Errorf("snapshot_id and tag are required")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(snapID); err != nil {
		return nil, fmt.Errorf("snapshot %q not found", snapID)
	}
	if err := store.TagSnapshot(snapID, tag); err != nil {
		return nil, err
	}
	return map[string]any{"snapshot_id": snapID, "tag": tag, "success": true}, nil
}

func toolUntagSnapshot(projectRoot string, args map[string]any) (any, error) {
	snapID := strArg(args, "snapshot_id")
	tag := strArg(args, "tag")
	if snapID == "" || tag == "" {
		return nil, fmt.Errorf("snapshot_id and tag are required")
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if err := store.UntagSnapshot(snapID, tag); err != nil {
		return nil, err
	}
	return map[string]any{"snapshot_id": snapID, "tag": tag, "success": true}, nil
}

// ─── Conflict resolution tools ───────────────────────────────────────────────

func toolListConflicts(projectRoot string, args map[string]any) (any, error) {
	// branch arg is accepted for documentation purposes; the scan is filesystem-based.
	_ = strArg(args, "branch")

	conflicts, err := mergepkg.ListConflicts(projectRoot)
	if err != nil {
		return nil, err
	}

	paths := make([]string, len(conflicts))
	for i, c := range conflicts {
		paths[i] = c.Path
	}
	return map[string]any{
		"conflicts":      paths,
		"conflict_count": len(conflicts),
		"resolved":       len(conflicts) == 0,
	}, nil
}

func toolResolveConflict(projectRoot string, args map[string]any) (any, error) {
	branchName := strArg(args, "branch")
	path := strArg(args, "path")
	resolution := strArg(args, "resolution")
	content := strArg(args, "content")

	if branchName == "" || path == "" || resolution == "" {
		return nil, fmt.Errorf("branch, path, and resolution are required")
	}

	if err := mergepkg.ResolveFile(projectRoot, branchName, path, resolution, content); err != nil {
		return nil, err
	}

	return map[string]any{
		"branch":     branchName,
		"path":       path,
		"resolution": resolution,
		"success":    true,
	}, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// strArg extracts a string value from an args map, returning "" if absent or wrong type.
func strArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// intArg extracts an integer value from an args map, returning 0 if absent or wrong type.
func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}
