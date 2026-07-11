// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/timeline"
	"github.com/spf13/cobra"
)

var (
	timelineSession string
	timelineBranch  string
	timelineLimit   int
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Show branch history as a story, grouped by agent session",
	Long: `Renders the active branch's snapshots grouped by the agent session that
produced them, each with a one-line change summary, interleaved with the
restores, merges, and undos from the operations log. This is the "what did
my agents do while I was away" report.

  avc timeline                     active branch, all sessions
  avc timeline --session <id>      one session's story
  avc timeline --branch <name>     a specific branch
  avc timeline --limit 100         show more snapshots (default 50)

Sessions come from the session_id/task attribution on snapshots
(avc snapshot --session <id> --task <desc>, or the MCP avc_snapshot
arguments). Unattributed snapshots appear under "(no session)".`,
	RunE: runTimeline,
}

func init() {
	timelineCmd.Flags().StringVar(&timelineSession, "session", "", "Show only this session ID")
	timelineCmd.Flags().StringVar(&timelineBranch, "branch", "", "Branch to show (default: active branch)")
	timelineCmd.Flags().IntVar(&timelineLimit, "limit", 0, "Max snapshots to include (default 50)")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branchName := timelineBranch
	if branchName == "" {
		branchName = branchpkg.GetActiveBranchName(projectPath)
	}

	result, err := timeline.Build(projectPath, branchName, timelineSession, timelineLimit)
	if err != nil {
		return fmt.Errorf("timeline: %w", err)
	}

	if jsonOutput {
		if result.Sessions == nil {
			result.Sessions = []timeline.Session{}
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	if len(result.Sessions) == 0 {
		if timelineSession != "" {
			fmt.Printf("No snapshots for session %s on branch %s.\n", bold(timelineSession), bold(branchName))
		} else {
			fmt.Printf("No snapshots on branch %s yet.\n", bold(branchName))
		}
		return nil
	}

	fmt.Printf("%s %s\n", prop("Timeline for branch:"), success(result.BranchName))
	for _, sess := range result.Sessions {
		fmt.Println()
		header := sess.SessionID
		if header == "" {
			header = "(no session)"
		}
		line := accent("● Session " + header)
		if len(sess.Agents) > 0 {
			line += dim("  ·  ") + green(strings.Join(sess.Agents, ", "))
		}
		fmt.Printf("  %s\n", line)
		if sess.Task != "" {
			fmt.Printf("    %s %s\n", prop("Task:"), bold(sess.Task))
		}
		for _, e := range sess.Events {
			ts := time.Unix(e.Timestamp, 0).Format("2006-01-02 15:04:05")
			if e.Kind == timeline.KindSnapshot {
				fmt.Printf("    %s  %s  %s\n", dim(ts), cyan(shortID(e.SnapshotID)), e.Label)
				if e.Summary != "" {
					fmt.Printf("    %s  %s\n", strings.Repeat(" ", len(ts)), dim(e.Summary))
				}
			} else {
				fmt.Printf("    %s  %s %s\n", dim(ts), yellow("⟲ "+e.OpKind), dim("— "+e.Details))
			}
		}
	}
	fmt.Println()
	return nil
}
