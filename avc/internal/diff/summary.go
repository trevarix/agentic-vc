// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// Heuristic change summaries: a human-readable one-liner per snapshot
// ("3 files: modified auth.go (+40 -12), added auth_test.go, deleted
// legacy.go") derived from the file diff against the previous branch HEAD.
// No LLM involved. Generated at snapshot creation and lazily by
// `avc timeline` for snapshots that predate summaries; per-file fragments
// are persisted in the diffs cache's change_summary column.

const (
	// summaryMaxCountedFiles caps how many modified files get line-counted
	// during summary generation. Line counts require reading both object
	// versions and an LCS pass, so a snapshot touching hundreds of files
	// must not stall on its own summary; files beyond the cap read
	// "modified" without counts.
	summaryMaxCountedFiles = 50
	// summaryMaxListedFiles is how many per-file fragments the one-liner
	// shows before truncating to "+N more".
	summaryMaxListedFiles = 3
)

// CacheSummaries computes a counts-only diff fromID→toID, persists one
// diffs-cache row per changed file with its change_summary fragment, and
// returns the file diffs. Row IDs are deterministic (from:to:path), so
// recomputing overwrites rather than duplicates.
func CacheSummaries(projectRoot, fromID, toID string) ([]*FileDiff, error) {
	res, err := compare(projectRoot, fromID, toID, enrichNone)
	if err != nil {
		return nil, err
	}

	// Line counts only for modified files (added/deleted fragments don't
	// show counts), capped so huge changesets stay cheap.
	counted := 0
	for _, fd := range res.Files {
		if fd.Type != Modified || counted >= summaryMaxCountedFiles {
			continue
		}
		enrichWithLineCounts(projectRoot, fd, enrichCounts)
		counted++
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	for _, fd := range res.Files {
		if err := store.UpsertDiffCache(&db.DiffCache{
			ID:             fmt.Sprintf("%s:%s:%s", fromID, toID, fd.Path),
			FromSnapshotID: fromID,
			ToSnapshotID:   toID,
			FilePath:       fd.Path,
			DiffType:       string(fd.Type),
			OldHash:        fd.OldHash,
			NewHash:        fd.NewHash,
			ChangeSummary:  FileSummary(fd),
		}); err != nil {
			return nil, err
		}
	}
	return res.Files, nil
}

// FileSummary renders one file's change as a standalone fragment, e.g.
// "modified src/auth.go (+40 -12)", "added auth_test.go", "deleted legacy.go".
func FileSummary(fd *FileDiff) string {
	frag := string(fd.Type) + " " + fd.Path
	if fd.Type == Modified {
		switch {
		case fd.Binary:
			frag += " (binary)"
		case fd.LinesAdded > 0 || fd.LinesRemoved > 0:
			frag += fmt.Sprintf(" (+%d -%d)", fd.LinesAdded, fd.LinesRemoved)
		}
	}
	return frag
}

// Summarize composes the snapshot-level one-liner from file diffs.
func Summarize(files []*FileDiff) string {
	frags := make([]string, len(files))
	for i, fd := range files {
		frags[i] = FileSummary(fd)
	}
	return composeSummary(frags)
}

// SummarizeCached composes the same one-liner from persisted diffs-cache
// rows, so `avc timeline` renders summaries without recomputing diffs.
func SummarizeCached(rows []*db.DiffCache) string {
	sorted := make([]*db.DiffCache, len(rows))
	copy(sorted, rows)
	order := map[string]int{string(Added): 0, string(Modified): 1, string(Deleted): 2}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if order[a.DiffType] != order[b.DiffType] {
			return order[a.DiffType] < order[b.DiffType]
		}
		return a.FilePath < b.FilePath
	})
	frags := make([]string, len(sorted))
	for i, r := range sorted {
		if r.ChangeSummary != "" {
			frags[i] = r.ChangeSummary
		} else {
			frags[i] = r.DiffType + " " + r.FilePath
		}
	}
	return composeSummary(frags)
}

func composeSummary(frags []string) string {
	n := len(frags)
	if n == 0 {
		return "no changes"
	}
	noun := "files"
	if n == 1 {
		noun = "file"
	}
	shown := frags
	extra := ""
	if n > summaryMaxListedFiles {
		shown = frags[:summaryMaxListedFiles]
		extra = fmt.Sprintf(", +%d more", n-summaryMaxListedFiles)
	}
	return fmt.Sprintf("%d %s: %s%s", n, noun, strings.Join(shown, ", "), extra)
}
