// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/spf13/cobra"
)

var (
	snapshotAgent   string
	snapshotNotes   string
	snapshotSession string
	snapshotTask    string
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot <label>",
	Short: "Create a snapshot of the current project state",
	Long: `Hashes all tracked files and saves a snapshot to the AVC database.
The label is a short human-readable description (e.g. "Before refactor").
The snapshot is associated with the currently active branch.`,
	Args: cobra.ExactArgs(1),
	RunE: runSnapshot,
}

var snapshotTagCmd = &cobra.Command{
	Use:   "tag <snapshot-id> <tag>",
	Short: "Apply a tag to a snapshot",
	Long: `Tags a snapshot with a machine-readable label (e.g. "stable", "v1.2.0").
Tags are searchable via avc list --tag <tag>.
Applying the same tag twice is a no-op.`,
	Args: cobra.ExactArgs(2),
	RunE: runSnapshotTag,
}

var snapshotUntagCmd = &cobra.Command{
	Use:   "untag <snapshot-id> <tag>",
	Short: "Remove a tag from a snapshot",
	Args:  cobra.ExactArgs(2),
	RunE:  runSnapshotUntag,
}

func init() {
	snapshotCmd.Flags().StringVar(&snapshotAgent, "agent", "", "Name of the agent creating this snapshot")
	snapshotCmd.Flags().StringVar(&snapshotNotes, "notes", "", "Optional notes for this snapshot")
	snapshotCmd.Flags().StringVar(&snapshotSession, "session", "", "Agent session ID this snapshot belongs to (see avc timeline)")
	snapshotCmd.Flags().StringVar(&snapshotTask, "task", "", "One-line description of the session's task")
	snapshotCmd.AddCommand(snapshotTagCmd, snapshotUntagCmd)
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	label := args[0]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branchID, err := branchpkg.GetActiveBranchID(projectPath)
	if err != nil {
		return fmt.Errorf("could not determine active branch: %w", err)
	}

	// For non-main branches use the workspace as the source directory so the
	// snapshot captures what the agent actually changed, not the real project root.
	branchName := branchpkg.GetActiveBranchName(projectPath)
	sourceDir := branchpkg.WorkspacePath(projectPath, branchName) // "" for main

	snap, err := snapshot.CreateWithOptions(projectPath, snapshot.Options{
		Label:     label,
		AgentName: snapshotAgent,
		Notes:     snapshotNotes,
		BranchID:  branchID,
		SourceDir: sourceDir,
		SessionID: snapshotSession,
		Task:      snapshotTask,
	})
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":            snap.ID,
			"label":         snap.Label,
			"timestamp":     snap.Timestamp,
			"agent_name":    snap.AgentName,
			"files_changed": snap.FileCount,
			"total_size":    snap.TotalSize,
			"notes":         snap.Notes,
			"branch_id":     snap.BranchID,
			"session_id":    snap.SessionID,
			"task":          snap.Task,
			"summary":       snap.Summary,
			"skipped_large": snap.SkippedLarge,
			"success":       true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Snapshot created:"), cyan(snap.ID))
	fmt.Printf("  %s %s\n", prop("Label:  "), bold(snap.Label))
	fmt.Printf("  %s %s\n", prop("Branch: "), green(branchpkg.GetActiveBranchName(projectPath)))
	fmt.Printf("  %s %s\n", prop("Files:  "), yellow(fmt.Sprintf("%d", snap.FileCount)))
	fmt.Printf("  %s %s\n", prop("Size:   "), dim(fmt.Sprintf("%d bytes", snap.TotalSize)))
	if len(snap.SkippedLarge) > 0 {
		fmt.Printf("  %s %s\n", prop("Skipped:"),
			yellow(fmt.Sprintf("%d file(s) exceeded the size limit (see stderr)", len(snap.SkippedLarge))))
	}
	return nil
}

func runSnapshotTag(cmd *cobra.Command, args []string) error {
	snapID, tag := args[0], args[1]
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Verify the snapshot exists.
	if _, err := store.GetSnapshot(snapID); err != nil {
		return fmt.Errorf("snapshot %q not found", snapID)
	}

	if err := store.TagSnapshot(snapID, tag); err != nil {
		return fmt.Errorf("tag: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"snapshot_id": snapID,
			"tag":         tag,
			"success":     true,
		})
	}

	fmt.Printf("%s %s %s %s\n",
		success("✓ Tagged snapshot"), cyan(shortID(snapID)),
		dim("with"), bold(tag),
	)
	return nil
}

func runSnapshotUntag(cmd *cobra.Command, args []string) error {
	snapID, tag := args[0], args[1]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.UntagSnapshot(snapID, tag); err != nil {
		return fmt.Errorf("untag: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"snapshot_id": snapID,
			"tag":         tag,
			"success":     true,
		})
	}

	fmt.Printf("%s %s %s %s\n",
		success("✓ Removed tag"), bold(tag),
		dim("from"), cyan(shortID(snapID)),
	)
	return nil
}

// shortID returns the first 12 chars of a snapshot ID followed by "…".
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

// formatTags renders a tag slice as a compact bracket-separated string.
func formatTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "[" + strings.Join(tags, ", ") + "]"
}
