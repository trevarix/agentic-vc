// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/bisect"
	"github.com/spf13/cobra"
)

var (
	bisectBranch  string
	bisectGood    string
	bisectGoodTag string
	bisectBad     string
	bisectCmd_    string
	bisectTimeout int
)

var bisectCmd = &cobra.Command{
	Use:   "bisect --good <snapshot-id> --cmd <command>",
	Short: "Find the first snapshot that broke a command (binary search)",
	Long: `Binary-searches the snapshot history between a known-good snapshot and a
bad one (default: branch HEAD) to find the first snapshot where the test
command fails — O(log n) runs instead of restoring snapshots one by one.

Each candidate is materialized into a throwaway scratch workspace and the
command runs through the same sandbox as avc run: environment scrubbing,
timeout, output caps. REQUIRES [run] enabled = true in .avc/config.toml —
bisect executes arbitrary commands, so a human must opt in.

Exit-code protocol (same as git bisect run):
  0     this snapshot is good
  125   cannot judge this snapshot (e.g. does not build) — skip it
  other this snapshot is bad

  avc bisect --good snap-abc --cmd "go test ./..."
  avc bisect --good-tag stable --bad snap-xyz --cmd "npm test"
  avc bisect --branch feat/auth --good snap-abc --cmd "pytest -x"`,
	RunE: runBisect,
}

func init() {
	bisectCmd.Flags().StringVar(&bisectBranch, "branch", "", "Branch to search (default: main)")
	bisectCmd.Flags().StringVar(&bisectGood, "good", "", "Known-good snapshot ID")
	bisectCmd.Flags().StringVar(&bisectGoodTag, "good-tag", "", "Use the newest snapshot with this tag as the known-good point")
	bisectCmd.Flags().StringVar(&bisectBad, "bad", "", "Known-bad snapshot ID (default: branch HEAD)")
	bisectCmd.Flags().StringVar(&bisectCmd_, "cmd", "", "Test command; exit 0 = good, 125 = skip, other = bad")
	bisectCmd.Flags().IntVar(&bisectTimeout, "timeout", 0, "Per-step timeout in seconds (default: sandbox default)")
	rootCmd.AddCommand(bisectCmd)
}

func runBisect(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	onStep := func(s bisect.Step) {
		if jsonOutput {
			// Stream progress as NDJSON so agents can observe long runs.
			_ = enc.Encode(map[string]any{
				"type":        "step",
				"snapshot_id": s.SnapshotID,
				"label":       s.Label,
				"exit_code":   s.ExitCode,
				"verdict":     s.Verdict,
				"remaining":   s.Remaining,
			})
		} else {
			mark := yellow("?")
			switch s.Verdict {
			case "good":
				mark = success("✓ good")
			case "bad":
				mark = failure("✗ bad ")
			case "skip":
				mark = yellow("~ skip")
			}
			fmt.Printf("  %s  %s  %s\n", mark, cyan(shortID(s.SnapshotID)), dim(s.Label))
		}
	}

	result, err := bisect.Run(projectPath, bisect.Options{
		BranchName:     bisectBranch,
		GoodID:         bisectGood,
		GoodTag:        bisectGoodTag,
		BadID:          bisectBad,
		Command:        bisectCmd_,
		TimeoutSeconds: bisectTimeout,
		OnStep:         onStep,
	})
	if err != nil {
		return fmt.Errorf("bisect: %w", err)
	}

	if jsonOutput {
		return enc.Encode(map[string]any{
			"type":            "result",
			"first_bad_id":    result.FirstBadID,
			"first_bad_label": result.FirstBadLabel,
			"predecessor_id":  result.PredecessorID,
			"steps":           result.Steps,
			"skipped":         result.Skipped,
			"summary":         result.Summary,
			"ambiguous":       result.Ambiguous,
			"message":         result.Message,
			"success":         true,
		})
	}

	fmt.Println()
	fmt.Printf("%s %s\n", failure("✗ First bad snapshot:"), cyan(result.FirstBadID))
	fmt.Printf("  %s %s\n", prop("Label:  "), bold(result.FirstBadLabel))
	fmt.Printf("  %s %s\n", prop("After:  "), cyan(result.PredecessorID))
	fmt.Printf("  %s %s\n", prop("Steps:  "), yellow(fmt.Sprintf("%d", result.Steps)))
	if result.Summary != "" {
		fmt.Printf("  %s %s\n", prop("Changed:"), result.Summary)
	}
	if result.Message != "" {
		fmt.Printf("\n%s %s\n", yellow("!"), result.Message)
	}
	fmt.Printf("\n%s\n", dim(fmt.Sprintf("Inspect with: avc diff %s %s", result.PredecessorID, result.FirstBadID)))
	return nil
}
