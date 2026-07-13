// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package merge

import (
	"fmt"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/policy"
	"github.com/trevarix/agentic-vc/avc/internal/undo"
	"github.com/trevarix/agentic-vc/avc/internal/workspace"
)

// Train branch statuses.
const (
	TrainMerged           = "merged"
	TrainConflicts        = "conflicts"
	TrainBlocked          = "blocked_protected"
	TrainValidationFailed = "validation_failed"
	TrainError            = "error"
	TrainSkipped          = "skipped"
)

// TrainBranchResult is the outcome for one branch in a merge train.
type TrainBranchResult struct {
	Branch              string `json:"branch"`
	Status              string `json:"status"` // merged | conflicts | blocked_protected | validation_failed | error | skipped
	PostMergeSnapshotID string `json:"post_merge_snapshot_id,omitempty"`
	Clean               int    `json:"clean,omitempty"`
	Merged              int    `json:"merged,omitempty"`
	Conflicts           int    `json:"conflicts,omitempty"`
	Detail              string `json:"detail,omitempty"` // conflict paths, validation output tail, error text
}

// TrainResult is the outcome of a whole merge train.
type TrainResult struct {
	Results   []TrainBranchResult `json:"results"`
	Completed int                 `json:"completed"`           // branches merged (and kept)
	StoppedAt string              `json:"stopped_at,omitempty"` // branch that halted the train; "" when all merged
}

// validateOutputTail caps how much validation output is carried in a result.
const validateOutputTail = 2000

// Train merges branches into main in order, each against the *current* main
// (so every branch sees the previous merges — the point of a train). The
// train stops at the first branch that conflicts, is blocked by [protect],
// or fails validation; main keeps the merges completed before the stop, each
// individually reversible via `avc undo` / its pre-merge snapshot.
//
// validate, when non-empty, runs after each merge against post-merge main
// through the workspace sandbox (requires [run] enabled, like every other
// command-execution surface). A failing validation rolls that one merge back
// (pre-merge snapshot restored, branch active again) and stops the train.
func Train(projectRoot string, branches []string, validate string, allowProtected bool) (*TrainResult, error) {
	if len(branches) == 0 {
		return nil, fmt.Errorf("at least one branch is required")
	}
	if validate != "" {
		cfg, _ := config.Load(projectRoot)
		if cfg == nil || !cfg.Run.Enabled {
			return nil, fmt.Errorf("--validate runs a command and requires [run] enabled = true in .avc/config.toml — a human must set this manually")
		}
	}

	train := &TrainResult{}
	stop := func(i int, r TrainBranchResult) *TrainResult {
		train.Results = append(train.Results, r)
		train.StoppedAt = r.Branch
		for _, rest := range branches[i+1:] {
			train.Results = append(train.Results, TrainBranchResult{Branch: rest, Status: TrainSkipped})
		}
		return train
	}

	for i, branchName := range branches {
		// Preview against current main first: conflicts and protected paths
		// are detected before anything is written, so a stopping train never
		// leaves conflict markers on main.
		plan, err := Preview(projectRoot, branchName)
		if err != nil {
			return stop(i, TrainBranchResult{Branch: branchName, Status: TrainError, Detail: err.Error()}), nil
		}
		if plan.Conflicts > 0 {
			return stop(i, TrainBranchResult{
				Branch:    branchName,
				Status:    TrainConflicts,
				Conflicts: plan.Conflicts,
				Detail:    conflictPaths(plan),
			}), nil
		}
		if len(plan.ProtectedChanges) > 0 && plan.ProtectedMode == policy.ModeBlock && !allowProtected {
			return stop(i, TrainBranchResult{
				Branch: branchName,
				Status: TrainBlocked,
				Detail: "protected paths: " + strings.Join(plan.ProtectedChanges, ", "),
			}), nil
		}

		res, err := MergeWithOptions(projectRoot, branchName, allowProtected)
		if err != nil {
			return stop(i, TrainBranchResult{Branch: branchName, Status: TrainError, Detail: err.Error()}), nil
		}
		if res.Conflicts > 0 {
			// The preview was clean but the merge conflicted (a dirty
			// workspace was auto-snapshotted in between). Conflict markers
			// are on main now — abort this merge so the train never leaves
			// main in a conflicted state.
			detail := conflictPaths(res)
			if abortErr := Abort(projectRoot); abortErr != nil {
				detail += " (rollback also failed: " + abortErr.Error() + " — run `avc merge --abort`)"
			}
			return stop(i, TrainBranchResult{
				Branch:    branchName,
				Status:    TrainConflicts,
				Conflicts: res.Conflicts,
				Detail:    detail,
			}), nil
		}

		if validate != "" {
			runRes, runErr := workspace.RunInDir(projectRoot, projectRoot, validate, 0)
			if runErr != nil || runRes.ExitCode != 0 {
				detail := ""
				if runErr != nil {
					detail = runErr.Error()
				} else {
					detail = fmt.Sprintf("exit code %d: %s", runRes.ExitCode,
						tail(strings.TrimSpace(runRes.Stderr+"\n"+runRes.Stdout), validateOutputTail))
				}
				// Roll back exactly this merge: undo restores main's
				// pre-merge snapshot, reactivates the branch, and rebuilds
				// its workspace (the newest logged operation is this merge).
				if _, undoErr := undo.Undo(projectRoot); undoErr != nil {
					detail += " (rollback also failed: " + undoErr.Error() + " — run `avc undo`)"
				}
				return stop(i, TrainBranchResult{
					Branch: branchName,
					Status: TrainValidationFailed,
					Detail: detail,
				}), nil
			}
		}

		train.Results = append(train.Results, TrainBranchResult{
			Branch:              branchName,
			Status:              TrainMerged,
			PostMergeSnapshotID: res.PostMergeSnapshotID,
			Clean:               res.Clean,
			Merged:              res.Merged,
		})
		train.Completed++
	}
	return train, nil
}

// conflictPaths lists a plan's conflicted paths for result details.
func conflictPaths(r *Result) string {
	var paths []string
	for _, f := range r.Files {
		if f.Decision == "conflict" {
			paths = append(paths, f.Path)
		}
	}
	return strings.Join(paths, ", ")
}

// tail returns the last n bytes of s (whole string when shorter).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
