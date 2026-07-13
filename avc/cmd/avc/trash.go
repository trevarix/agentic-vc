// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/trash"
	"github.com/spf13/cobra"
)

var trashOlderThan string

var trashCmd = &cobra.Command{
	Use:   "trash",
	Short: "Inspect files quarantined by restore instead of deleted",
	Long: `AVC never permanently deletes untracked files during a restore — they are
moved to .avc/trash/ instead, so nothing a restore removes is unrecoverable.

Use 'avc trash list' to see quarantined files, or 'avc trash empty' to
permanently reclaim the space once you're sure you don't need them.`,
}

var trashListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quarantined trash entries, newest first",
	RunE:  runTrashList,
}

var trashEmptyCmd = &cobra.Command{
	Use:   "empty",
	Short: "Permanently delete quarantined trash entries",
	Long: `By default removes all trash entries. Pass --older-than to only remove
entries older than a given duration (e.g. "24h", "168h" for 7 days).`,
	RunE: runTrashEmpty,
}

var trashRestoreCmd = &cobra.Command{
	Use:   "restore <op_id> [path]",
	Short: "Move quarantined files back to where they came from",
	Long: `Restores files from a trash entry to their original location (recorded when
they were quarantined). Pass a specific relative path to restore one file, or
omit it to restore the whole entry. Files that already exist at the
destination are skipped, never overwritten.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runTrashRestore,
}

func init() {
	trashEmptyCmd.Flags().StringVar(&trashOlderThan, "older-than", "",
		`Only remove entries older than this duration (e.g. "24h"); default removes all`)
	trashCmd.AddCommand(trashListCmd, trashEmptyCmd, trashRestoreCmd)
}

func runTrashRestore(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	opID := args[0]
	path := ""
	if len(args) == 2 {
		path = args[1]
	}

	restored, skipped, err := trash.Restore(projectPath, opID, path)
	if err != nil {
		return fmt.Errorf("trash restore: %w", err)
	}

	if jsonOutput {
		if restored == nil {
			restored = []string{}
		}
		if skipped == nil {
			skipped = []string{}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"op_id":    opID,
			"restored": restored,
			"skipped":  skipped,
			"success":  true,
		})
	}

	for _, f := range restored {
		fmt.Printf("%s %s\n", success("✓ Restored:"), f)
	}
	for _, f := range skipped {
		fmt.Printf("%s %s %s\n", warn("⚠ Skipped:"), f, dim("(a file already exists there — not overwritten)"))
	}
	if len(restored) == 0 && len(skipped) == 0 {
		fmt.Printf("%s\n", dim("Nothing to restore."))
	}
	return nil
}

func runTrashList(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	entries, err := trash.List(projectPath)
	if err != nil {
		return fmt.Errorf("list trash: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Printf("%s\n", dim("Trash is empty."))
		return nil
	}

	for _, e := range entries {
		fmt.Printf("%s %s  %s\n",
			prop(e.OpID), dim("·"), dim(e.CreatedAt.Format("2006-01-02 15:04:05")))
		for _, f := range e.Files {
			fmt.Printf("    %s\n", f)
		}
	}
	fmt.Printf("\nRun %s to permanently delete these.\n", cyan("avc trash empty"))
	return nil
}

func runTrashEmpty(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	var olderThan time.Duration
	if trashOlderThan != "" {
		olderThan, err = time.ParseDuration(trashOlderThan)
		if err != nil {
			return fmt.Errorf("invalid --older-than duration: %w", err)
		}
	}

	removed, err := trash.Empty(projectPath, olderThan)
	if err != nil {
		return fmt.Errorf("empty trash: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"removed": removed,
			"success": true,
		})
	}

	fmt.Printf("%s %d entr(ies) removed.\n", success("✓"), removed)
	return nil
}
