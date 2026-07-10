package avc

import (
	"encoding/json"
	"fmt"
	"os"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/policy"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show files changed since the last snapshot",
	Long: `Compares the current working tree against the last snapshot on the active branch.

On an agent branch this compares the branch workspace, not the real project root.
Output mirrors 'git status': one line per changed file with A/M/D prefix and line counts.

Use --json for machine-readable output.`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branchName := branchpkg.GetActiveBranchName(projectPath)
	sourceDir := branchpkg.WorkspacePath(projectPath, branchName) // "" for main
	if sourceDir == "" {
		sourceDir = projectPath
	}

	branchID, err := branchpkg.GetActiveBranchID(projectPath)
	if err != nil {
		return fmt.Errorf("could not determine active branch: %w", err)
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	head, headErr := store.GetHeadSnapshot(branchID)
	store.Close()

	if headErr != nil {
		fmt.Fprintln(os.Stderr, "No snapshots yet. Run `avc snapshot` to start tracking.")
		return nil
	}

	result, err := diff.CompareWithCurrentDir(projectPath, sourceDir, head.ID)
	if err != nil {
		return err
	}

	if jsonOutput {
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
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"branch":        branchName,
			"snapshot_id":   head.ID,
			"snapshot_label": head.Label,
			"files":         files,
			"changed_count": len(result.Files),
		})
	}

	if len(result.Files) == 0 {
		fmt.Printf("%s Nothing changed since %s\n",
			success("✓"), cyan(fmt.Sprintf("%q", head.Label)))
		return nil
	}

	// Protected-path early warning: a changed file matching [protect] is
	// marked with "!" so the collision is visible long before merge time.
	cfg, _ := config.Load(projectPath)
	protectedSet := map[string]bool{}
	if policy.Enabled(cfg) {
		var paths []string
		for _, f := range result.Files {
			paths = append(paths, f.Path)
		}
		for _, p := range policy.Check(cfg, paths) {
			protectedSet[p] = true
		}
	}

	fmt.Printf("%s %s  %s %s\n\n",
		accent("◆ Status:"), cyan(branchName),
		dim("·  last snapshot:"), dim(fmt.Sprintf("%q", head.Label)))

	symbols := map[string]string{
		"added":    green("A"),
		"modified": yellow("M"),
		"deleted":  red("D"),
	}
	for _, f := range result.Files {
		sym := symbols[string(f.Type)]
		mark := " "
		if protectedSet[f.Path] {
			mark = failure("!")
		}
		if f.Binary {
			fmt.Printf(" %s%s  %-40s  %s\n", mark, sym, f.Path, dim("binary file"))
			continue
		}
		fmt.Printf(" %s%s  %-40s  %s %s\n",
			mark, sym, f.Path,
			green(fmt.Sprintf("+%d", f.LinesAdded)),
			red(fmt.Sprintf("-%d", f.LinesRemoved)),
		)
	}
	fmt.Printf("\n%s  Run %s to save.\n",
		yellow(fmt.Sprintf("%d file(s) changed.", len(result.Files))),
		cyan(fmt.Sprintf("avc snapshot \"<label>\"")),
	)
	if len(protectedSet) > 0 {
		fmt.Printf("%s\n", warn(fmt.Sprintf(
			"⚠ %d changed file(s) marked ! are protected ([protect] in .avc/config.toml) — a merge touching them will be gated.",
			len(protectedSet),
		)))
	}
	return nil
}
