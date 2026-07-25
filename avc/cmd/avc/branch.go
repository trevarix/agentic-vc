// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/spf13/cobra"
)

var (
	branchFromSnapshot string
	branchFromBranch   string
)

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Manage branches (agent workspaces)",
	Long: `Branches let agents work in isolation without affecting main.
Each branch is rooted at a base snapshot and accumulates its own snapshot history.`,
}

var branchCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new branch",
	Long: `Creates a new branch (agent workspace) and switches to it.

By default the branch is rooted at the HEAD snapshot of main. Use --from to
root it at a specific snapshot, or --from-branch <parent> to stack it on
another branch's current HEAD (the child starts from the parent's latest
work; merging it still targets main).`,
	Args: cobra.ExactArgs(1),
	RunE: runBranchCreate,
}

var (
	branchListAll    bool
	branchListStatus string
)

var branchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List branches (active by default)",
	Long: `Lists branches. By default, only active branches are shown.

Use --all to include merged and abandoned branches.
Use --status to filter by a specific status: active, merged, or abandoned.`,
	RunE: runBranchList,
}

var branchSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch the active branch",
	Long: `Updates the active branch.
Does not modify the working directory — use avc restore to roll the project
state to a specific snapshot on the target branch.`,
	Args: cobra.ExactArgs(1),
	RunE: runBranchSwitch,
}

var branchDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a branch and its snapshot history",
	Args:  cobra.ExactArgs(1),
	RunE:  runBranchDelete,
}

var branchDeleteKeepHistory bool

var branchDiffStatMode bool

var branchDiffCmd = &cobra.Command{
	Use:   "diff [branch | a..b]",
	Short: "Show a branch's cumulative diff, or compare two branches",
	Long: `Shows all changes made on the branch since it was created.

With the a..b form, compares two branches' HEAD snapshots instead — useful
before merging parallel agent branches to see how their work differs:

  avc branch diff feat/auth              base → HEAD of feat/auth
  avc branch diff feat/auth..feat/api    HEAD of feat/auth → HEAD of feat/api
  avc branch diff main..feat/auth        main HEAD → branch HEAD

Use --stat for a compact summary (file names + line counts only).
Use --json for machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBranchDiff,
}

var branchRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a branch",
	Args:  cobra.ExactArgs(2),
	RunE:  runBranchRename,
}

var branchAbandonCmd = &cobra.Command{
	Use:   "abandon <name>",
	Short: "Mark a branch as abandoned (keeps history, removes nothing)",
	Args:  cobra.ExactArgs(1),
	RunE:  runBranchAbandon,
}

var branchPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove workspaces for merged branches",
	Long:  `Deletes workspace directories for all branches with status 'merged'. DB records and snapshots are kept.`,
	RunE:  runBranchPrune,
}

var branchPruneMerged bool

func init() {
	branchCreateCmd.Flags().StringVar(&branchFromSnapshot, "from", "", "Base snapshot ID (defaults to HEAD of main)")
	branchCreateCmd.Flags().StringVar(&branchFromBranch, "from-branch", "", "Stack on another branch: use its HEAD snapshot as the base")
	branchDeleteCmd.Flags().BoolVar(&branchDeleteKeepHistory, "keep-history", false, "Retain snapshot rows; do not cascade-delete them")
	branchDiffCmd.Flags().BoolVar(&branchDiffStatMode, "stat", false, "Show compact summary (file names + line counts) instead of full diff")
	branchListCmd.Flags().BoolVar(&branchListAll, "all", false, "Show all branches including merged and abandoned")
	branchListCmd.Flags().StringVar(&branchListStatus, "status", "", "Filter by status: active, merged, abandoned")
	branchPruneCmd.Flags().BoolVar(&branchPruneMerged, "merged", false, "Remove workspaces for all merged branches")
	branchCmd.AddCommand(
		branchCreateCmd,
		branchListCmd,
		branchSwitchCmd,
		branchDeleteCmd,
		branchDiffCmd,
		branchRenameCmd,
		branchAbandonCmd,
		branchPruneCmd,
	)
}

func runBranchCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if branchFromSnapshot != "" && branchFromBranch != "" {
		return fmt.Errorf("--from and --from-branch are mutually exclusive")
	}
	var b *db.Branch
	if branchFromBranch != "" {
		b, err = branch.CreateFromBranch(projectPath, name, branchFromBranch)
	} else {
		b, err = branch.Create(projectPath, name, branchFromSnapshot)
	}
	if err != nil {
		return fmt.Errorf("branch create: %w", err)
	}

	// Auto-switch to the new branch — creating a branch means you're about to work on it.
	if err := branch.Switch(projectPath, name); err != nil {
		return fmt.Errorf("branch create succeeded but auto-switch failed: %w", err)
	}

	workspacePath := branch.WorkspacePath(projectPath, b.Name)

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":               b.ID,
			"name":             b.Name,
			"base_snapshot_id": b.BaseSnapshotID,
			"parent_branch":    branchFromBranch,
			"created_at":       b.CreatedAt,
			"workspace":        workspacePath,
			"active":           true,
			"success":          true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Branch created and switched:"), cyan(b.Name))
	fmt.Printf("  %s %s\n", prop("Workspace:"), green(workspacePath))
	if branchFromBranch != "" {
		fmt.Printf("  %s %s\n", prop("Stacked on:"), cyan(branchFromBranch))
	}
	fmt.Printf("\n%s\n", dim("Direct your agent to work in the workspace directory."))
	return nil
}

func runBranchList(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	// Resolve which status filter to apply.
	statusFilter := "active" // default: only active
	if branchListAll {
		statusFilter = "" // all statuses
	} else if branchListStatus != "" {
		statusFilter = branchListStatus
	}

	branches, err := branch.ListByStatus(projectPath, statusFilter)
	if err != nil {
		return err
	}

	activeName := branch.GetActiveBranchName(projectPath)

	// Resolve parent-branch IDs to names for lineage display. Parents may
	// have any status, so look them up across all branches.
	parentName := map[string]string{}
	if all, allErr := branch.ListByStatus(projectPath, ""); allErr == nil {
		byID := make(map[string]string, len(all))
		for _, b := range all {
			byID[b.ID] = b.Name
		}
		for _, b := range branches {
			if b.ParentBranchID != "" {
				parentName[b.ID] = byID[b.ParentBranchID]
			}
		}
	}

	if jsonOutput {
		type branchJSON struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			BaseSnapshotID string `json:"base_snapshot_id"`
			ParentBranch   string `json:"parent_branch,omitempty"`
			CreatedAt      int64  `json:"created_at"`
			Status         string `json:"status"`
			Active         bool   `json:"active"`
			Workspace      string `json:"workspace"`
		}
		out := make([]branchJSON, len(branches))
		for i, b := range branches {
			out[i] = branchJSON{
				ID:             b.ID,
				Name:           b.Name,
				BaseSnapshotID: b.BaseSnapshotID,
				ParentBranch:   parentName[b.ID],
				CreatedAt:      b.CreatedAt,
				Status:         b.Status,
				Active:         b.Name == activeName,
				Workspace:      branch.WorkspacePath(projectPath, b.Name),
			}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(branches) == 0 {
		fmt.Println(dim("No branches found for the current filter."))
		return nil
	}

	for _, b := range branches {
		ts := time.Unix(b.CreatedAt, 0).Format("2006-01-02 15:04")
		statusStr := ""
		switch b.Status {
		case "merged":
			statusStr = "  " + dim("[merged]")
		case "abandoned":
			statusStr = "  " + yellow("[abandoned]")
		}
		if p := parentName[b.ID]; p != "" {
			statusStr += "  " + dim("(from "+p+")")
		}
		if b.Name == activeName {
			fmt.Printf("* %s  %s%s\n", bold(green(b.Name)), dim(ts), statusStr)
		} else {
			fmt.Printf("  %s  %s%s\n", b.Name, dim(ts), statusStr)
		}
	}
	return nil
}

func runBranchRename(cmd *cobra.Command, args []string) error {
	oldName, newName := args[0], args[1]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Rename(projectPath, oldName, newName); err != nil {
		return fmt.Errorf("branch rename: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"old_name": oldName,
			"new_name": newName,
			"success":  true,
		})
	}

	fmt.Printf("%s %s %s %s\n", success("✓ Renamed branch"), cyan(oldName), dim("→"), cyan(newName))
	return nil
}

func runBranchAbandon(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Abandon(projectPath, name); err != nil {
		return fmt.Errorf("branch abandon: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":    name,
			"status":  "abandoned",
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Marked branch as abandoned:"), bold(name))
	return nil
}

func runBranchPrune(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	pruned, err := branch.PruneMergedWorkspaces(projectPath)
	if err != nil {
		return fmt.Errorf("branch prune: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"pruned":  pruned,
			"count":   len(pruned),
			"success": true,
		})
	}

	if len(pruned) == 0 {
		fmt.Println(dim("No merged branch workspaces to prune."))
		return nil
	}
	for _, name := range pruned {
		fmt.Printf("%s %s\n", success("✓ Pruned workspace:"), cyan(name))
	}
	fmt.Printf("%s\n", dim(fmt.Sprintf("Removed %d workspace(s). DB records and snapshots are retained.", len(pruned))))
	return nil
}

func runBranchSwitch(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Switch(projectPath, name); err != nil {
		return fmt.Errorf("branch switch: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":    name,
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Switched to branch"), cyan(name))
	fmt.Printf("%s\n", dim("Working directory unchanged — use `avc restore <id>` to restore a snapshot."))
	return nil
}

func runBranchDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Delete(projectPath, name, branchDeleteKeepHistory); err != nil {
		return fmt.Errorf("branch delete: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":    name,
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Deleted branch:"), bold(name))
	return nil
}

func runBranchDiff(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	name := branch.GetActiveBranchName(projectPath)
	if len(args) > 0 {
		name = args[0]
	}

	// a..b form: compare two branches' HEAD snapshots.
	if from, to, ok := strings.Cut(name, ".."); ok {
		if from == "" || to == "" {
			return fmt.Errorf("both sides of '..' need a branch name (e.g. feat/a..feat/b)")
		}
		return runCrossBranchDiff(projectPath, from, to)
	}

	branches, err := branch.List(projectPath)
	if err != nil {
		return err
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
		return fmt.Errorf("branch '%s' not found", name)
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	// When no base snapshot exists (branch created before any main snapshot),
	// fall back to the oldest snapshot on the branch as the diff base.
	if baseSnapshotID == "" {
		oldest, err := store.GetOldestSnapshot(branchID)
		if err != nil {
			store.Close()
			return fmt.Errorf("branch '%s' has no snapshots yet", name)
		}
		baseSnapshotID = oldest.ID
	}
	head, err := store.GetHeadSnapshot(branchID)
	store.Close()
	if err != nil {
		return fmt.Errorf("branch '%s' has no snapshots yet", name)
	}

	// Stat mode shows counts only, so skip building the (potentially large)
	// unified-diff previews — unless --json is requested, which includes them.
	var result *diff.Result
	if branchDiffStatMode && !jsonOutput {
		result, err = diff.CompareCounts(projectPath, baseSnapshotID, head.ID)
	} else {
		result, err = diff.Compare(projectPath, baseSnapshotID, head.ID)
	}
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	if jsonOutput {
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
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"branch":        name,
			"from_snapshot": baseSnapshotID,
			"to_snapshot":   head.ID,
			"files":         files,
		})
	}

	if branchDiffStatMode {
		printDiffStat(result.Files)
		return nil
	}

	fmt.Printf("%s %s\n%s\n\n", accent("◆ Branch diff:"), cyan(name), ruler(50))
	printBranchDiffFiles(result.Files)
	return nil
}

// runCrossBranchDiff compares the HEAD snapshots of two branches (a..b form).
func runCrossBranchDiff(projectPath, fromName, toName string) error {
	fromHead, err := branchHeadSnapshotID(projectPath, fromName)
	if err != nil {
		return err
	}
	toHead, err := branchHeadSnapshotID(projectPath, toName)
	if err != nil {
		return err
	}

	result, err := diff.Compare(projectPath, fromHead, toHead)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"from_branch":   fromName,
			"to_branch":     toName,
			"from_snapshot": fromHead,
			"to_snapshot":   toHead,
			"files":         branchDiffFilesJSON(result.Files),
		})
	}

	if branchDiffStatMode {
		printDiffStat(result.Files)
		return nil
	}

	fmt.Printf("%s %s %s %s\n%s\n\n",
		accent("◆ Branch diff:"), cyan(fromName), dim("→"), cyan(toName), ruler(50))
	printBranchDiffFiles(result.Files)
	return nil
}

// branchHeadSnapshotID resolves a branch name (including "main") to its HEAD
// snapshot ID.
func branchHeadSnapshotID(projectPath, name string) (string, error) {
	store, err := db.Open(projectPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	proj, err := store.GetProject(projectPath)
	if err != nil {
		return "", err
	}
	b, err := store.GetBranchByName(proj.ID, name)
	if err != nil {
		return "", fmt.Errorf("branch '%s' not found", name)
	}
	head, err := store.GetHeadSnapshot(b.ID)
	if err != nil {
		return "", fmt.Errorf("branch '%s' has no snapshots yet", name)
	}
	return head.ID, nil
}

// printBranchDiffFiles renders the shared per-file diff listing used by both
// branch-diff forms.
func printBranchDiffFiles(files []*diff.FileDiff) {
	for _, f := range files {
		symbol, pathColor := changeSymbol(string(f.Type))
		if f.Binary {
			fmt.Printf("%s %s (%s)\n", symbol, pathColor(f.Path), dim("binary file"))
			continue
		}
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf("%s %s (%s %s)\n", symbol, pathColor(f.Path), added, removed)
	}
	if len(files) == 0 {
		fmt.Println("No differences.")
	}
}

// branchDiffFilesJSON converts diff files to the JSON shape shared by both
// branch-diff forms.
func branchDiffFilesJSON(files []*diff.FileDiff) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, f := range files {
		out[i] = map[string]any{
			"path":             f.Path,
			"type":             string(f.Type),
			"old_hash":         f.OldHash,
			"new_hash":         f.NewHash,
			"lines_added":      f.LinesAdded,
			"lines_removed":    f.LinesRemoved,
			"diff_preview":     f.DiffPreview,
			"binary":           f.Binary,
			"counts_estimated": f.CountsEstimated,
		}
	}
	return out
}
