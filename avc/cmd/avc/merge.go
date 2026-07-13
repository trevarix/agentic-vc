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
var mergeAllowProtected bool
var mergeTrain bool
var mergeValidate string

var mergeCmd = &cobra.Command{
	Use:   "merge <branch> | --train <branch>...",
	Short: "Merge an agent branch back into main",
	Long: `Performs a three-way merge of <branch> into main.

AVC auto-snapshots main before writing anything, so you can always abort.

Decisions per file:
  clean    — only the branch changed; applied automatically
  merged   — both sides changed different regions; combined line-by-line
  conflict — overlapping changes; written with conflict markers
  skip     — no net change; left untouched

With --train, merges several branches in order, each against the current
main (so each merge sees the previous ones). The train stops at the first
conflict; completed merges are kept, each reversible via avc undo.
--validate "<command>" runs after every merge — a failure rolls that merge
back and stops the train (requires [run] enabled, like avc run):

  avc merge --train feat/a feat/b feat/c --validate "go test ./..."`,
	Args: cobra.ArbitraryArgs,
	RunE: runMerge,
}

func init() {
	mergeCmd.Flags().BoolVar(&mergePreview, "preview", false, "Preview the merge without applying changes")
	mergeCmd.Flags().BoolVar(&mergeAbort, "abort", false, "Abort the last in-progress merge")
	mergeCmd.Flags().BoolVar(&mergeAllowProtected, "allow-protected", false,
		"Proceed even if the merge changes [protect] paths (human override; agents cannot pass this)")
	mergeCmd.Flags().BoolVar(&mergeTrain, "train", false, "Merge multiple branches in order, stopping at the first conflict")
	mergeCmd.Flags().StringVar(&mergeValidate, "validate", "",
		"Command to run after each --train merge; failure rolls that merge back and stops (requires [run] enabled)")
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

	if mergeTrain {
		if len(args) == 0 {
			return fmt.Errorf("--train requires at least one branch name")
		}
		return runMergeTrain(projectPath, args)
	}

	if len(args) == 0 {
		return fmt.Errorf("branch name is required (or use --abort)")
	}
	if len(args) > 1 {
		return fmt.Errorf("merge takes one branch (use --train to merge several in order)")
	}
	branchName := args[0]

	if mergeValidate != "" {
		return fmt.Errorf("--validate only applies to --train")
	}

	if mergePreview {
		result, err := mergepkg.Preview(projectPath, branchName)
		if err != nil {
			return err
		}
		printMergeResult(result, true)
		return nil
	}

	result, err := mergepkg.MergeWithOptions(projectPath, branchName, mergeAllowProtected)
	if err != nil {
		return err
	}
	printMergeResult(result, false)
	return nil
}

func runMergeTrain(projectPath string, branches []string) error {
	result, err := mergepkg.Train(projectPath, branches, mergeValidate, mergeAllowProtected)
	if err != nil {
		return fmt.Errorf("merge train: %w", err)
	}

	if jsonOutput {
		out, _ := json.MarshalIndent(map[string]any{
			"results":    result.Results,
			"completed":  result.Completed,
			"stopped_at": result.StoppedAt,
			"success":    result.StoppedAt == "",
		}, "", "  ")
		fmt.Println(string(out))
		if result.StoppedAt != "" {
			return errTrainStopped
		}
		return nil
	}

	fmt.Printf("%s %d branch(es)\n\n", accent("◆ Merge train:"), len(branches))
	for _, r := range result.Results {
		switch r.Status {
		case mergepkg.TrainMerged:
			line := fmt.Sprintf("  %s  %s", success("✓ merged           "), cyan(r.Branch))
			if r.Merged > 0 {
				line += dim(fmt.Sprintf("  (%d clean, %d line-merged)", r.Clean, r.Merged))
			}
			fmt.Println(line)
		case mergepkg.TrainConflicts:
			fmt.Printf("  %s  %s  %s\n", failure("✗ conflicts        "), cyan(r.Branch), dim(r.Detail))
		case mergepkg.TrainBlocked:
			fmt.Printf("  %s  %s  %s\n", failure("✗ blocked          "), cyan(r.Branch), dim(r.Detail))
		case mergepkg.TrainValidationFailed:
			fmt.Printf("  %s  %s  %s\n", failure("✗ validation failed"), cyan(r.Branch), dim(r.Detail))
		case mergepkg.TrainError:
			fmt.Printf("  %s  %s  %s\n", failure("✗ error            "), cyan(r.Branch), dim(r.Detail))
		case mergepkg.TrainSkipped:
			fmt.Printf("  %s  %s\n", dim("- skipped          "), dim(r.Branch))
		}
	}

	fmt.Println()
	if result.StoppedAt == "" {
		fmt.Printf("%s All %d branch(es) merged into main.\n", success("✓ Train complete."), result.Completed)
		return nil
	}
	fmt.Printf("%s Stopped at %s after %d merge(s).\n",
		warn("⚠ Train stopped."), bold(result.StoppedAt), result.Completed)
	fmt.Println(dim("  Completed merges are kept — each is reversible with `avc undo`."))
	return errTrainStopped
}

// errTrainStopped gives a stopped train a non-zero exit code without cobra
// re-printing a redundant message (the report above already explains it).
var errTrainStopped = fmt.Errorf("merge train stopped before completing all branches")

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
		}
		if len(result.ProtectedChanges) > 0 {
			m["protected_changes"] = result.ProtectedChanges
			m["protected_mode"] = result.ProtectedMode
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
	if result.Merged > 0 {
		fmt.Printf("  %s %s %s\n", prop("Merged:   "), success(fmt.Sprintf("%d", result.Merged)), dim("file(s) combined line-by-line (both sides changed, no overlap)"))
	}
	if result.Deleted > 0 {
		fmt.Printf("  %s %s %s\n", prop("Deleted:  "), yellow(fmt.Sprintf("%d", result.Deleted)), dim("file(s) removed (deleted on branch)"))
	}
	fmt.Printf("  %s %s %s\n", prop("Conflicts:"), conflictStr, dim("file(s) need manual resolution"))
	fmt.Printf("  %s %s %s\n", prop("Skipped:  "), skippedStr, dim("file(s) unchanged"))

	if preview && result.WorkspaceDirtyFiles > 0 {
		fmt.Printf("\n  %s\n", warn(fmt.Sprintf(
			"⚠ %d file(s) in the workspace changed since the last snapshot and are NOT reflected in this preview.",
			result.WorkspaceDirtyFiles,
		)))
		fmt.Printf("  %s\n", dim("Run `avc snapshot` first if you want them included."))
	}

	if len(result.ProtectedChanges) > 0 {
		fmt.Printf("\n  %s\n", warn(fmt.Sprintf(
			"⚠ This merge changes %d protected path(s) ([protect] in .avc/config.toml):",
			len(result.ProtectedChanges),
		)))
		for _, p := range result.ProtectedChanges {
			fmt.Printf("    %s %s\n", failure("!"), red(p))
		}
		if preview && result.ProtectedMode == "block" {
			fmt.Printf("  %s\n", dim("The merge will be refused unless a human runs it with --allow-protected."))
		}
	}

	if result.AutoSnapshotID != "" {
		fmt.Printf("\n  %s %s\n",
			success("✓ Captured un-snapshotted workspace changes:"),
			dim(result.AutoSnapshotID[:12]+"…"),
		)
	}

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

	if result.Clean > 0 || result.Merged > 0 || result.Deleted > 0 || result.Conflicts > 0 {
		fmt.Printf("\n%s\n", ruler(50))
		for _, f := range result.Files {
			switch f.Decision {
			case "skip":
				continue
			case "conflict":
				fmt.Printf("  %s  %s  %s\n", failure("!"), red(f.Path), dim("conflict"))
			case "merged":
				fmt.Printf("  %s  %s  %s\n", success("✓"), green(f.Path), dim("merged line-by-line"))
			case "delete":
				fmt.Printf("  %s  %s  %s\n", yellow("-"), yellow(f.Path), dim("deleted"))
			default:
				fmt.Printf("  %s  %s  %s\n", success("✓"), green(f.Path), dim("applied"))
			}
		}
	}
}
