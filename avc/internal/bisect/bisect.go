// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bisect finds the first snapshot that broke a command, in O(log n)
// test runs. Each candidate is materialized into a throwaway scratch
// workspace (`.avc/workspaces/.bisect-<id>/` — the leading dot keeps it out
// of branch listings, and ValidateBranchName rejects user branches with
// leading dots so there is no collision) and the command runs through the
// same sandbox as avc_run_in_workspace, gated on the same [run] enabled
// switch.
package bisect

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
	"github.com/trevarix/agentic-vc/avc/internal/restore"
	"github.com/trevarix/agentic-vc/avc/internal/workspace"
)

// SkipExitCode marks a candidate that cannot be judged (e.g. unbuildable),
// mirroring git bisect's convention.
const SkipExitCode = 125

const (
	scratchPrefix = ".bisect-"
	// staleScratchAge is how old an abandoned scratch workspace must be
	// before the sweep on the next run removes it (an interrupted bisect
	// can't clean up after itself).
	staleScratchAge = time.Hour
)

// Step reports one completed test run to the progress callback.
type Step struct {
	SnapshotID string `json:"snapshot_id"`
	Label      string `json:"label"`
	ExitCode   int    `json:"exit_code"`
	Verdict    string `json:"verdict"` // "good" | "bad" | "skip"
	Remaining  int    `json:"remaining"` // candidates still in the search window
}

// Options configures a bisect run.
type Options struct {
	BranchName     string // default "main"
	GoodID         string // required unless GoodTag is set
	GoodTag        string // alternative to GoodID: newest snapshot carrying this tag
	BadID          string // default: branch HEAD
	Command        string // required; runs through the workspace sandbox
	TimeoutSeconds int    // per-step timeout (0 = sandbox default)
	OnStep         func(Step)
}

// Result is the outcome of a bisect run.
type Result struct {
	FirstBadID    string           `json:"first_bad_id"`
	FirstBadLabel string           `json:"first_bad_label"`
	PredecessorID string           `json:"predecessor_id"`
	Steps         int              `json:"steps"`
	Skipped       []string         `json:"skipped,omitempty"`
	Summary       string           `json:"summary,omitempty"` // change summary of the first bad snapshot
	Diff          *diffpkg.Result  `json:"-"`                 // first bad snapshot vs its predecessor
	Ambiguous     bool             `json:"ambiguous"`         // skips prevented exact narrowing
	Message       string           `json:"message,omitempty"`
}

// Run executes the bisect. The [run] enabled gate applies exactly as it does
// for avc_run_in_workspace — this runs arbitrary commands, so a human must
// have opted in.
func Run(projectRoot string, opts Options) (*Result, error) {
	cfg, _ := config.Load(projectRoot)
	if cfg == nil || !cfg.Run.Enabled {
		return nil, fmt.Errorf("bisect runs commands and requires [run] enabled = true in .avc/config.toml — a human must set this manually")
	}
	if strings.TrimSpace(opts.Command) == "" {
		return nil, fmt.Errorf("a test command is required (--cmd)")
	}

	branchName := opts.BranchName
	if branchName == "" {
		branchName = "main"
	}

	// Load the branch's snapshots in chronological order.
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		return nil, err
	}
	branch, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		store.Close()
		return nil, err
	}
	snaps, err := store.ListSnapshotsByBranch(branch.ID) // newest first
	if err != nil {
		store.Close()
		return nil, err
	}
	goodID := opts.GoodID
	if goodID == "" && opts.GoodTag != "" {
		tagged, tagErr := store.ListSnapshotsByTag(opts.GoodTag) // newest first
		if tagErr == nil {
			for _, s := range tagged {
				if s.BranchID == branch.ID {
					goodID = s.ID
					break
				}
			}
		}
		if goodID == "" {
			store.Close()
			return nil, fmt.Errorf("no snapshot on branch %q carries tag %q", branchName, opts.GoodTag)
		}
	}
	store.Close()

	if goodID == "" {
		return nil, fmt.Errorf("a known-good snapshot is required (--good <id> or --good-tag <tag>)")
	}

	// Reverse to oldest-first.
	for l, r := 0, len(snaps)-1; l < r; l, r = l+1, r-1 {
		snaps[l], snaps[r] = snaps[r], snaps[l]
	}

	goodIdx, badIdx := -1, -1
	for i, s := range snaps {
		if s.ID == goodID {
			goodIdx = i
		}
		if opts.BadID != "" && s.ID == opts.BadID {
			badIdx = i
		}
	}
	if goodIdx == -1 {
		return nil, fmt.Errorf("good snapshot %q not found on branch %q", goodID, branchName)
	}
	if opts.BadID == "" {
		badIdx = len(snaps) - 1 // branch HEAD
	} else if badIdx == -1 {
		return nil, fmt.Errorf("bad snapshot %q not found on branch %q", opts.BadID, branchName)
	}
	if badIdx <= goodIdx {
		return nil, fmt.Errorf("the bad snapshot must be newer than the good one (good=%s, bad=%s)", snaps[goodIdx].ID, snaps[badIdx].ID)
	}

	// Scratch workspace: swept, created, and removed around the run.
	sweepStaleScratch(projectRoot)
	scratch := scratchPrefix + newRunID()
	scratchDir := filepath.Join(projectRoot, ".avc", "workspaces", scratch)
	defer os.RemoveAll(scratchDir)

	verdicts := map[int]string{goodIdx: "good", badIdx: "bad"}
	skippedIdx := map[int]bool{}
	var skipped []string
	steps := 0

	test := func(idx int) (string, error) {
		s := snaps[idx]
		// Fresh materialization per candidate: no leftovers from the
		// previous step can contaminate the verdict.
		if err := os.RemoveAll(scratchDir); err != nil {
			return "", fmt.Errorf("reset scratch workspace: %w", err)
		}
		if err := os.MkdirAll(scratchDir, 0755); err != nil {
			return "", err
		}
		if _, err := restore.RestoreToDir(projectRoot, s.ID, scratchDir); err != nil {
			return "", fmt.Errorf("materialize %s: %w", s.ID, err)
		}
		res, err := workspace.Run(workspace.RunRequest{
			ProjectRoot:    projectRoot,
			BranchName:     scratch,
			Command:        opts.Command,
			TimeoutSeconds: opts.TimeoutSeconds,
		})
		if err != nil {
			return "", err
		}
		if res.ExitCode == -1 {
			// Killed on timeout or refused by the sandbox — neither is a
			// verdict about the snapshot. Guessing here would misdirect the
			// whole search.
			return "", fmt.Errorf("command did not produce a verdict at %s (timeout or blocked): %s", s.ID, strings.TrimSpace(res.Stderr))
		}
		steps++
		verdict := "bad"
		switch {
		case res.ExitCode == 0:
			verdict = "good"
		case res.ExitCode == SkipExitCode:
			verdict = "skip"
		}
		if opts.OnStep != nil {
			opts.OnStep(Step{
				SnapshotID: s.ID,
				Label:      s.Label,
				ExitCode:   res.ExitCode,
				Verdict:    verdict,
				Remaining:  badIdx - goodIdx - 1,
			})
		}
		return verdict, nil
	}

	// Classic bisect loop: invariant — snaps[goodIdx] is good, snaps[badIdx]
	// is bad; the first bad snapshot is in (goodIdx, badIdx].
	for badIdx-goodIdx > 1 {
		mid := pickCandidate(goodIdx, badIdx, skippedIdx)
		if mid == -1 {
			break // every remaining candidate was skipped
		}
		verdict, err := test(mid)
		if err != nil {
			return nil, err
		}
		verdicts[mid] = verdict
		switch verdict {
		case "good":
			goodIdx = mid
		case "bad":
			badIdx = mid
		case "skip":
			skippedIdx[mid] = true
			skipped = append(skipped, snaps[mid].ID)
		}
	}

	firstBad := snaps[badIdx]
	pred := snaps[goodIdx]
	result := &Result{
		FirstBadID:    firstBad.ID,
		FirstBadLabel: firstBad.Label,
		PredecessorID: pred.ID,
		Steps:         steps,
		Skipped:       skipped,
	}
	if badIdx-goodIdx > 1 {
		result.Ambiguous = true
		result.Message = fmt.Sprintf(
			"%d skipped snapshot(s) between the last good and first bad could not be tested — the true first bad snapshot may be one of them",
			badIdx-goodIdx-1)
	}

	// Enrich with what changed in the culprit.
	if d, err := diffpkg.Compare(projectRoot, pred.ID, firstBad.ID); err == nil {
		result.Diff = d
		result.Summary = diffpkg.Summarize(d.Files)
	}
	return result, nil
}

// pickCandidate returns the untested index nearest the midpoint of
// (good, bad), or -1 when every index in the window is skipped.
func pickCandidate(good, bad int, skipped map[int]bool) int {
	mid := (good + bad) / 2
	// Probe outward from the midpoint: mid, mid+1, mid-1, mid+2, …
	for offset := 0; ; offset++ {
		hi := mid + offset
		lo := mid - offset
		inWindow := false
		if hi > good && hi < bad {
			inWindow = true
			if !skipped[hi] {
				return hi
			}
		}
		if lo > good && lo < bad {
			inWindow = true
			if !skipped[lo] {
				return lo
			}
		}
		if !inWindow {
			return -1
		}
	}
}

// sweepStaleScratch removes scratch workspaces older than staleScratchAge —
// leftovers from interrupted runs. Fresh ones are left alone in case a
// concurrent bisect owns them.
func sweepStaleScratch(projectRoot string) {
	base := filepath.Join(projectRoot, ".avc", "workspaces")
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), scratchPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleScratchAge {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
}

func newRunID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
