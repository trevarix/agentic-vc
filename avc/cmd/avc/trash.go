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

func init() {
	trashEmptyCmd.Flags().StringVar(&trashOlderThan, "older-than", "",
		`Only remove entries older than this duration (e.g. "24h"); default removes all`)
	trashCmd.AddCommand(trashListCmd, trashEmptyCmd)
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
