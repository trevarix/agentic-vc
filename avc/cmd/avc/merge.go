package avc

import (
	"encoding/json"
	"fmt"

	mergepkg "github.com/SkillMythOrg/agentic-vc/avc/internal/merge"
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
  skip     — no net change; left untouched

Flags:
  --preview  Show what would happen without writing any files
  --abort    Restore main from the pre-merge snapshot and mark merge aborted`,
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
			fmt.Println("Merge aborted. Main restored to pre-merge snapshot.")
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
		out, _ := json.MarshalIndent(map[string]any{
			"merge_id":   result.MergeID,
			"branch":     result.BranchName,
			"preview":    preview,
			"clean":      result.Clean,
			"conflicts":  result.Conflicts,
			"skipped":    result.Skipped,
			"files":      files,
		}, "", "  ")
		fmt.Println(string(out))
		return
	}

	action := "Merge result"
	if preview {
		action = "Merge preview"
	}
	fmt.Printf("%s for branch '%s':\n", action, result.BranchName)
	fmt.Printf("  Clean:     %d file(s) will be applied automatically\n", result.Clean)
	fmt.Printf("  Conflicts: %d file(s) need manual resolution\n", result.Conflicts)
	fmt.Printf("  Skipped:   %d file(s) unchanged\n", result.Skipped)

	if result.Conflicts > 0 && !preview {
		fmt.Println("\nConflicts written with markers (<<<<<<< / ======= / >>>>>>>).")
		fmt.Println("Resolve them, then snapshot to record the resolution.")
		fmt.Println("Or run: avc merge --abort  to undo and restore main.")
	}

	if result.Clean > 0 || result.Conflicts > 0 {
		fmt.Println("\nFiles:")
		for _, f := range result.Files {
			if f.Decision == "skip" {
				continue
			}
			marker := "✓"
			if f.Decision == "conflict" {
				marker = "!"
			}
			fmt.Printf("  %s  %s  (%s)\n", marker, f.Path, f.Decision)
		}
	}
}
