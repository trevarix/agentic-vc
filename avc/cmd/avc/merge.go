// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"

	mergepkg "github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/spf13/cobra"
)

var mergePreview bool
var mergeAbort bool

var mergeCmd = &cobra.Command{
	Use:   "merge <branch>",
	Short: "Merge an agent branch back into main",
	Long: `Performs a three-way merge of <branch> into main.

AVC auto-snapshots main before writing anything, so you can always abort.

Decisions per file:
  clean    — only the branch changed; applied automatically
  conflict — both main and branch changed; written with conflict markers
  skip     — no net change; left untouched`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMerge,
}

func init() {
	mergeCmd.Flags().BoolVar(&mergePreview, "preview", false, "Preview the merge without applying changes")
	mergeCmd.Flags().BoolVar(&mergeAbort, "abort", false, "Abort the last in-progress merge")
}

func runMerge(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if mergeAbort {
		if err := mergepkg.Abort(projectPath); err != nil {
			return err
		}
		if jsonOutput {
			out, _ := json.MarshalIndent(map[string]any{"aborted": true}, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Printf("%s Main restored to pre-merge snapshot.\n", warn("⚠ Merge aborted."))
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("branch name is required (or use --abort)")
	}
	branchName := args[0]

	if mergePreview {
		result, err := mergepkg.Preview(projectPath, branchName)
		if err != nil {
			return err
		}
		printMergeResult(result, true)
		return nil
	}

	result, err := mergepkg.Merge(projectPath, branchName)
	if err != nil {
		return err
	}
	printMergeResult(result, false)
	return nil
}

func printMergeResult(result *mergepkg.Result, preview bool) {
	if jsonOutput {
		type fileJSON struct {
			Path     string `json:"path"`
			Decision string `json:"decision"`
		}
		files := make([]fileJSON, len(result.Files))
		for i, f := range result.Files {
			files[i] = fileJSON{f.Path, f.Decision}
		}
		m := map[string]any{
			"merge_id":  result.MergeID,
			"branch":    result.BranchName,
			"preview":   preview,
			"clean":     result.Clean,
			"conflicts": result.Conflicts,
			"skipped":   result.Skipped,
			"files":     files,
		}
		if result.PostMergeSnapshotID != "" {
			m["post_merge_snapshot_id"] = result.PostMergeSnapshotID
		}
		out, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(out))
		return
	}

	action := "Merge result"
	if preview {
		action = "Merge preview"
	}
	fmt.Printf("%s %s\n\n", accent("◆ "+action+":"), cyan(result.BranchName))

	cleanStr := success(fmt.Sprintf("%d", result.Clean))
	conflictStr := failure(fmt.Sprintf("%d", result.Conflicts))
	skippedStr := dim(fmt.Sprintf("%d", result.Skipped))
	fmt.Printf("  %s %s %s\n", prop("Clean:    "), cleanStr, dim("file(s) applied automatically"))
	fmt.Printf("  %s %s %s\n", prop("Conflicts:"), conflictStr, dim("file(s) need manual resolution"))
	fmt.Printf("  %s %s %s\n", prop("Skipped:  "), skippedStr, dim("file(s) unchanged"))

	if result.Conflicts > 0 && !preview {
		fmt.Printf("\n  %s\n", warn("⚠ Conflicts written with markers (<<<<<<< / ======= / >>>>>>>)."))
		fmt.Printf("  %s\n", dim("Resolve them, then snapshot to record the resolution."))
		fmt.Printf("  %s\n", dim("Or run: avc merge --abort  to undo and restore main."))
	}

	if result.PostMergeSnapshotID != "" {
		fmt.Printf("\n  %s %s\n",
			success("✓ Post-merge snapshot:"),
			dim(result.PostMergeSnapshotID[:12]+"…"),
		)
		fmt.Printf("  %s\n", dim("Main is now at the merged state. Run `avc list` to confirm."))
	}

	if result.Clean > 0 || result.Conflicts > 0 {
		fmt.Printf("\n%s\n", ruler(50))
		for _, f := range result.Files {
			if f.Decision == "skip" {
				continue
			}
			if f.Decision == "conflict" {
				fmt.Printf("  %s  %s  %s\n", failure("!"), red(f.Path), dim("conflict"))
			} else {
				fmt.Printf("  %s  %s  %s\n", success("✓"), green(f.Path), dim("applied"))
			}
		}
	}
}
