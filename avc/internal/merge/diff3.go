// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Line-level three-way merge (diff3). Given a common base and two derived
// versions, regions changed on only one side merge automatically; only
// regions changed differently on both sides become conflict hunks — instead
// of the whole-file conflicts the hash-only decision table produces.
package merge

import (
	"bytes"

	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
)

// Diff3 performs a line-level three-way merge of main and branch relative to
// their common ancestor base.
//
// merged is the full resulting content: cleanly merged regions plus, for
// each region both sides changed differently, a diff3-style conflict hunk
// using the same markers as whole-file conflicts (so ListConflicts and
// resolution tooling treat them identically). conflicts is the number of
// such hunks (0 = clean merge).
//
// ok is false when no line merge was attempted — either side exceeded the
// diff size cap — and the caller should fall back to a whole-file conflict.
// Callers are responsible for excluding binary content before calling.
func Diff3(base, main, branch []byte) (merged []byte, conflicts int, ok bool) {
	baseLines := splitKeepEnds(base)
	mainLines := splitKeepEnds(main)
	branchLines := splitKeepEnds(branch)

	matchMain := buildLineMatch(baseLines, mainLines)
	matchBranch := buildLineMatch(baseLines, branchLines)
	if matchMain == nil || matchBranch == nil {
		return nil, 0, false // over the size cap — no line merge attempted
	}

	chunks, aligned := alignChunks(baseLines, mainLines, branchLines, matchMain, matchBranch)
	if !aligned {
		return nil, 0, false
	}

	var out bytes.Buffer
	for _, c := range chunks {
		if c.stable {
			writeLines(&out, c.base)
			continue
		}
		switch {
		case linesEqual(c.main, c.base): // only branch changed
			writeLines(&out, c.branch)
		case linesEqual(c.branch, c.base): // only main changed
			writeLines(&out, c.main)
		case linesEqual(c.main, c.branch): // both changed identically
			writeLines(&out, c.main)
		default: // both changed differently — conflict hunk
			conflicts++
			writeConflictHunk(&out, c)
		}
	}
	return out.Bytes(), conflicts, true
}

// chunk is one aligned region of the three files: either a stable run where
// all three agree (base holds the lines) or an unstable region holding each
// side's lines between two sync points.
type chunk struct {
	stable             bool
	base, main, branch []string
}

// alignChunks walks the three files in lockstep using the base→main and
// base→branch line matches, emitting alternating stable and unstable chunks.
// This is the classic diff3 alignment: a stable run requires the same base
// line to be matched at the current offset in *both* derived files; between
// stable runs, everything up to the next base line matched in both sides
// forms one unstable region.
func alignChunks(base, main, branch []string, matchMain, matchBranch map[int]int) ([]chunk, bool) {
	lb, lm, lr := len(base), len(main), len(branch)
	var chunks []chunk

	i, m, r := 0, 0, 0
	for {
		// Extend a stable run: base[i+k] matched to exactly main[m+k] and branch[r+k].
		k := 0
		for i+k < lb {
			mi, okM := matchMain[i+k]
			ri, okR := matchBranch[i+k]
			if !okM || !okR || mi != m+k || ri != r+k {
				break
			}
			k++
		}
		if k > 0 {
			chunks = append(chunks, chunk{stable: true, base: base[i : i+k]})
			i, m, r = i+k, m+k, r+k
			continue
		}

		if i >= lb && m >= lm && r >= lr {
			return chunks, true
		}

		// Find the next base line matched in BOTH sides — the next possible
		// sync point. Everything before it (on all three sides) is one
		// unstable region.
		j := i
		for j < lb {
			_, okM := matchMain[j]
			_, okR := matchBranch[j]
			if okM && okR {
				break
			}
			j++
		}
		mEnd, rEnd := lm, lr
		if j < lb {
			mEnd, rEnd = matchMain[j], matchBranch[j]
		}
		if mEnd < m || rEnd < r || (j == i && mEnd == m && rEnd == r) {
			// Matches should be monotonic and every iteration must consume
			// input; bail to a whole-file conflict rather than looping.
			return nil, false
		}
		chunks = append(chunks, chunk{
			base:   base[i:j],
			main:   main[m:mEnd],
			branch: branch[r:rEnd],
		})
		i, m, r = j, mEnd, rEnd
	}
}

// buildLineMatch returns base-line-index → other-line-index for every line
// the LCS pairs up, or nil when either file exceeds the diff size cap
// (diffpkg.ComputeEdits declines and returns nil in that case).
func buildLineMatch(base, other []string) map[int]int {
	if len(base) == 0 || len(other) == 0 {
		return map[int]int{}
	}
	edits := diffpkg.ComputeEdits(base, other)
	if edits == nil {
		return nil
	}
	match := make(map[int]int)
	bi, oi := 0, 0
	for _, e := range edits {
		switch e.Op {
		case diffpkg.EditKeep:
			match[bi] = oi
			bi++
			oi++
		case diffpkg.EditDelete:
			bi++
		case diffpkg.EditAdd:
			oi++
		}
	}
	return match
}

// splitKeepEnds splits content into lines, each retaining its original
// terminator ("\n" or "\r\n"), so a clean merge reassembles byte-identical
// content — no line-ending normalization is ever written back to disk. A
// final unterminated line is kept as-is.
func splitKeepEnds(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var lines []string
	start := 0
	for idx, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:idx+1]))
			start = idx + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
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

func writeLines(out *bytes.Buffer, lines []string) {
	for _, l := range lines {
		out.WriteString(l)
	}
}

// writeConflictHunk renders one conflicted region with the same diff3-style
// markers writeConflict uses for whole-file conflicts, so conflict scanning
// (ListConflicts) and resolution flows treat hunk conflicts identically.
func writeConflictHunk(out *bytes.Buffer, c chunk) {
	ensureNL := func() {
		if b := out.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	ensureNL()
	out.WriteString("<<<<<<< main (ours)\n")
	writeLines(out, c.main)
	ensureNL()
	out.WriteString("||||||| base (common ancestor)\n")
	writeLines(out, c.base)
	ensureNL()
	out.WriteString("=======\n")
	writeLines(out, c.branch)
	ensureNL()
	out.WriteString(">>>>>>> branch (theirs)\n")
}
