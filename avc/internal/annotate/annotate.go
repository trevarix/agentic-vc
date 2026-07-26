// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package annotate traces line origins across snapshots for a single file.
package annotate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/diff"
)

// LineAnnotation describes which snapshot introduced a specific line.
type LineAnnotation struct {
	Line       int    `json:"line"`
	SnapshotID string `json:"snapshot_id"`
	Label      string `json:"label"`
	AgentName  string `json:"agent_name"`
	Timestamp  int64  `json:"timestamp"`
}

// AnnotateResult is the full annotation for a file.
type AnnotateResult struct {
	FilePath   string            `json:"file_path"`
	TotalLines int               `json:"total_lines"`
	Lines      []*LineAnnotation `json:"lines"`
}

// Block is a contiguous run of lines that share an originating snapshot.
type Block struct {
	Start int             // 1-based first line, inclusive
	End   int             // 1-based last line, inclusive
	Line  *LineAnnotation // annotation shared by every line in the block
}

// CollapseBlocks groups per-line annotations into runs of consecutive lines
// that share a snapshot (blame-style), so a file is described one block at a
// time instead of one line at a time. Assumes lines are ordered by line number.
func CollapseBlocks(lines []*LineAnnotation) []Block {
	var blocks []Block
	for _, ln := range lines {
		if n := len(blocks); n > 0 &&
			blocks[n-1].Line.SnapshotID == ln.SnapshotID &&
			ln.Line == blocks[n-1].End+1 {
			blocks[n-1].End = ln.Line
			continue
		}
		blocks = append(blocks, Block{Start: ln.Line, End: ln.Line, Line: ln})
	}
	return blocks
}

// ClassifyAuthor labels a line's originating snapshot as agent- or
// human-authored. Human-origin: no agent name, or the automatic save-snapshots
// ("auto") that capture a human's own edits. Everything else is a named AI
// agent (e.g. "claude", "cursor", or the MCP default "agent").
func ClassifyAuthor(agentName string) (label string, isAgent bool) {
	name := strings.TrimSpace(agentName)
	if name == "" || strings.EqualFold(name, "auto") {
		return "you", false
	}
	return name, true
}

// Annotate traces line origins for filePath across all snapshots.
// filePath must be a slash-separated relative path within the project.
//
// Previously this fired one GetSnapshotFiles query per snapshot (O(N) queries).
// It now fires exactly one GetFileVersions query that joins files + snapshots,
// then fetches snapshot metadata (label, agent_name) in a single ListSnapshots
// call — total: 2 queries regardless of history length.
func Annotate(projectRoot, filePath string) (*AnnotateResult, error) {
	filePath = filepath.ToSlash(filepath.Clean(filePath))

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// One query: all (snapshot_id, hash, timestamp) pairs for this file, oldest-first.
	versions, err := store.GetFileVersions(filePath)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		// File was never tracked. Try to annotate from current disk content.
		return annotateCurrentOnly(projectRoot, filePath)
	}

	// Fetch snapshot metadata (label, agent_name) keyed by snapshot ID.
	// One query for all snapshots — avoids per-version lookups.
	allSnaps, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	snapMeta := make(map[string]*db.Snapshot, len(allSnaps))
	for _, s := range allSnaps {
		snapMeta[s.ID] = s
	}

	// ── Forward walk: track which snapshot introduced each line ──────────────

	// Start with the first version: all lines attributed to the first snapshot.
	firstData := diff.ReadObjectSafe(projectRoot, versions[0].FileHash)
	firstLines := diff.SplitLines(firstData)

	origins := make([]string, len(firstLines)) // origins[i] = snapshot ID for line i
	for i := range origins {
		origins[i] = versions[0].SnapshotID
	}

	for v := 1; v < len(versions); v++ {
		if versions[v].FileHash == versions[v-1].FileHash {
			continue // content unchanged — skip
		}

		oldData := diff.ReadObjectSafe(projectRoot, versions[v-1].FileHash)
		newData := diff.ReadObjectSafe(projectRoot, versions[v].FileHash)
		oldLines := diff.SplitLines(oldData)
		newLines := diff.SplitLines(newData)

		edits := diff.ComputeEdits(oldLines, newLines)
		if edits == nil {
			// File too large for LCS — attribute all lines to this snapshot.
			origins = make([]string, len(newLines))
			for i := range origins {
				origins[i] = versions[v].SnapshotID
			}
			continue
		}

		newOrigins := make([]string, 0, len(newLines))
		oldIdx := 0
		for _, e := range edits {
			switch e.Op {
			case diff.EditKeep:
				if oldIdx < len(origins) {
					newOrigins = append(newOrigins, origins[oldIdx])
				} else {
					newOrigins = append(newOrigins, versions[v].SnapshotID)
				}
				oldIdx++
			case diff.EditAdd:
				newOrigins = append(newOrigins, versions[v].SnapshotID)
			case diff.EditDelete:
				oldIdx++
			}
		}
		origins = newOrigins
	}

	// ── Optionally compare latest snapshot with current disk file ─────────────

	latestHash := versions[len(versions)-1].FileHash
	latestSnapID := versions[len(versions)-1].SnapshotID
	absPath := filepath.Join(projectRoot, filepath.FromSlash(filePath))
	currentData, err := os.ReadFile(absPath)
	if err == nil {
		currentLines := diff.SplitLines(currentData)
		storedData := diff.ReadObjectSafe(projectRoot, latestHash)
		storedLines := diff.SplitLines(storedData)

		if !linesEqual(storedLines, currentLines) {
			edits := diff.ComputeEdits(storedLines, currentLines)
			if edits != nil {
				newOrigins := make([]string, 0, len(currentLines))
				oldIdx := 0
				for _, e := range edits {
					switch e.Op {
					case diff.EditKeep:
						if oldIdx < len(origins) {
							newOrigins = append(newOrigins, origins[oldIdx])
						} else {
							newOrigins = append(newOrigins, latestSnapID)
						}
						oldIdx++
					case diff.EditAdd:
						newOrigins = append(newOrigins, latestSnapID)
					case diff.EditDelete:
						oldIdx++
					}
				}
				origins = newOrigins
			}
		}
	}

	// ── Build result ─────────────────────────────────────────────────────────

	result := &AnnotateResult{
		FilePath:   filePath,
		TotalLines: len(origins),
		Lines:      make([]*LineAnnotation, len(origins)),
	}
	for i, snapID := range origins {
		ann := &LineAnnotation{
			Line:       i + 1,
			SnapshotID: snapID,
		}
		if meta, ok := snapMeta[snapID]; ok {
			ann.Label = meta.Label
			ann.AgentName = meta.AgentName
			ann.Timestamp = meta.Timestamp
		}
		result.Lines[i] = ann
	}

	return result, nil
}

// annotateCurrentOnly returns a result for a file that exists on disk but was
// never included in any snapshot.
func annotateCurrentOnly(projectRoot, filePath string) (*AnnotateResult, error) {
	absPath := filepath.Join(projectRoot, filepath.FromSlash(filePath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("file '%s' not found on disk or in any snapshot", filePath)
	}
	lines := diff.SplitLines(data)

	result := &AnnotateResult{
		FilePath:   filePath,
		TotalLines: len(lines),
		Lines:      make([]*LineAnnotation, len(lines)),
	}
	for i := range lines {
		result.Lines[i] = &LineAnnotation{
			Line:       i + 1,
			SnapshotID: "",
			Label:      "(untracked)",
			AgentName:  "",
			Timestamp:  0,
		}
	}
	return result, nil
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
