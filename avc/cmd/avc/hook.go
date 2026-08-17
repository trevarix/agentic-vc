// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/agenthook"
	"github.com/spf13/cobra"
)

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

Reads a Claude Code PreToolUse payload as JSON on stdin and uses its
session_id to decide: the first edit of a session produces a checkpoint,
every later edit is a no-op. Snapshotting every edit would flood avc list
and leave avc timeline unreadable.

Does nothing outside an AVC project, and always exits 0 — a hook that
blocked an edit would be worse than no hook at all.

Wire it up as a PreToolUse hook matching Write|Edit:

  { "type": "command", "command": "avc hook pre-edit" }`,
	Args: cobra.NoArgs,
	RunE: runHookPreEdit,
}

func init() {
	hookCmd.AddCommand(hookPreEditCmd)
}

func runHookPreEdit(cmd *cobra.Command, args []string) error {
	in, err := readHookInput(os.Stdin)
	if err != nil {
		return reportHookFailure(fmt.Errorf("read hook payload: %w", err))
	}

	result, err := agenthook.PreEdit(in)
	if err != nil {
		return reportHookFailure(err)
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
	var in agenthook.Input
	err := json.NewDecoder(r).Decode(&in)
	if errors.Is(err, io.EOF) {
		return agenthook.Input{}, nil
	}
	if err != nil {
		return agenthook.Input{}, err
	}
	return in, nil
}

// reportHookFailure writes the failure to stderr and returns nil so the process
// still exits 0. A hook handler must never turn an AVC problem into a blocked
// edit, so this deliberately breaks the usual "errors propagate up" rule.
func reportHookFailure(err error) error {
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(agenthook.Result{
			Action: agenthook.ActionSkipped,
			Reason: err.Error(),
		})
	}
	fmt.Fprintf(os.Stderr, "[avc] warning: pre-edit hook skipped: %v\n", err)
	return nil
}
