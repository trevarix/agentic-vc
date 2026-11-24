// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/trevarix/agentic-vc/avc/internal/agenthook"
)

// hookAgent names the harness that invoked the hook, for attribution.
var hookAgent string

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Handlers invoked by an agent harness's hook system",
	Long: `Commands designed to be called by an agent harness, not by hand.

Each handler reads its harness payload as JSON on stdin and always exits 0,
so a failure inside AVC can never block the agent's work.`,
}

var hookPreEditCmd = &cobra.Command{
	Use:   "pre-edit",
	Short: "Checkpoint the project before an agent's first edit of a session",
	Long: `Takes one snapshot per agent session, capturing the project as it stood
before that session edited anything.

Reads the harness's hook payload as JSON on stdin and uses its session id
to decide: the first edit of a session produces a checkpoint, every later
edit is a no-op. Snapshotting every edit would flood avc list and leave
avc timeline unreadable. Payloads from Claude Code, Codex, Copilot (VS Code
and CLI), Windsurf (trajectory_id) and Cursor (conversation_id) are read.

Does nothing outside an AVC project, and always exits 0 — a hook that
blocked an edit would be worse than no hook at all.

avc init --skills <tool> --hooks installs it for you. By hand, for Claude
Code, wire it up as a PreToolUse hook matching Write|Edit:

  { "type": "command", "command": "avc hook pre-edit" }`,
	Args: cobra.NoArgs,
	RunE: runHookPreEdit,
}

func init() {
	hookPreEditCmd.Flags().StringVar(&hookAgent, "agent", "",
		"Harness that invoked the hook, used to attribute the checkpoint (default claude-code)")
	hookCmd.AddCommand(hookPreEditCmd)
}

func runHookPreEdit(cmd *cobra.Command, args []string) error {
	in, err := readHookInput(os.Stdin)
	if err != nil {
		return reportHookFailure(fmt.Errorf("read hook payload: %w", err))
	}
	in.Harness = hookAgent

	result, err := agenthook.PreEdit(in)
	if err != nil {
		return reportHookFailure(err)
	}

	reply, quiet := agenthook.HarnessReply(hookAgent)
	if quiet {
		// The harness reads stdout as its decision: answer it, and keep the
		// report on stderr where it is shown as a log line, if at all.
		fmt.Fprintf(os.Stderr, "[avc] pre-edit hook: %s %s\n", result.Action, result.SnapshotID+result.Reason)
		if reply != "" {
			fmt.Println(reply)
		}
		return nil
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if result.Action == agenthook.ActionSnapshotted {
		fmt.Printf("%s %s\n", success("✓ Session checkpoint:"), cyan(result.SnapshotID))
		return nil
	}
	fmt.Printf("%s %s\n", dim("· No checkpoint taken:"), dim(result.Reason))
	return nil
}

// readHookInput decodes the harness payload. Empty stdin is not an error: it
// yields a zero Input, which PreEdit skips for want of a session ID.
func readHookInput(r io.Reader) (agenthook.Input, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return agenthook.Input{}, err
	}
	return agenthook.ParseInput(data)
}

// reportHookFailure writes the failure to stderr and returns nil so the process
// still exits 0. A hook handler must never turn an AVC problem into a blocked
// edit, so this deliberately breaks the usual "errors propagate up" rule.
func reportHookFailure(err error) error {
	if reply, quiet := agenthook.HarnessReply(hookAgent); quiet {
		fmt.Fprintf(os.Stderr, "[avc] warning: pre-edit hook skipped: %v\n", err)
		if reply != "" {
			fmt.Println(reply)
		}
		return nil
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(agenthook.Result{
			Action: agenthook.ActionSkipped,
			Reason: err.Error(),
		})
	}
	fmt.Fprintf(os.Stderr, "[avc] warning: pre-edit hook skipped: %v\n", err)
	return nil
}
