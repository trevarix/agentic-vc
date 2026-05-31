// Package diff computes file-level differences between two snapshots.
package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/db"
)


// ChangeType classifies how a file changed between two snapshots.
type ChangeType string

const (
	Added    ChangeType = "added"
	Deleted  ChangeType = "deleted"
	Modified ChangeType = "modified"
)

// FileDiff describes the change to a single file.
type FileDiff struct {
	Path         string
	Type         ChangeType
	OldHash      string
	NewHash      string
	LinesAdded   int
	LinesRemoved int
	DiffPreview  string // unified diff excerpt
}

// Result is the full diff between two snapshots.
type Result struct {
	FromSnapshotID string
	ToSnapshotID   string
	Files          []*FileDiff
}

// Compare computes the diff between fromID and toID snapshots.
func Compare(projectRoot, fromID, toID string) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	if _, err := store.GetSnapshot(fromID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", fromID)
	}
	if _, err := store.GetSnapshot(toID); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", toID)
	}

	fromFiles, err := store.GetSnapshotFiles(fromID)
	if err != nil {
		return nil, err
	}
	toFiles, err := store.GetSnapshotFiles(toID)
	if err != nil {
		return nil, err
	}

	fromMap := filesByPath(fromFiles)
	toMap := filesByPath(toFiles)

	var diffs []*FileDiff

	for path, toFile := range toMap {
		fromFile, exists := fromMap[path]
		if !exists {
			fd := &FileDiff{
				Path:    path,
				Type:    Added,
				NewHash: toFile.FileHash,
			}
			enrichWithLineCounts(projectRoot, fd)
			diffs = append(diffs, fd)
			continue
		}
		if fromFile.FileHash != toFile.FileHash {
			fd := &FileDiff{
				Path:    path,
				Type:    Modified,
				OldHash: fromFile.FileHash,
				NewHash: toFile.FileHash,
			}
			enrichWithLineCounts(projectRoot, fd)
			diffs = append(diffs, fd)
		}
	}

	for path, fromFile := range fromMap {
		if _, exists := toMap[path]; !exists {
			fd := &FileDiff{
				Path:    path,
				Type:    Deleted,
				OldHash: fromFile.FileHash,
			}
			enrichWithLineCounts(projectRoot, fd)
			diffs = append(diffs, fd)
		}
	}

	sortDiffs(diffs)

	return &Result{
		FromSnapshotID: fromID,
		ToSnapshotID:   toID,
		Files:          diffs,
	}, nil
}

func filesByPath(files []*db.File) map[string]*db.File {
	m := make(map[string]*db.File, len(files))
	for _, f := range files {
		m[f.RelativePath] = f
	}
	return m
}

func enrichWithLineCounts(projectRoot string, fd *FileDiff) {
	oldData := ReadObjectSafe(projectRoot, fd.OldHash)
	newData := ReadObjectSafe(projectRoot, fd.NewHash)

	added, removed, preview := computeUnifiedDiff(SplitLines(oldData), SplitLines(newData))
	fd.LinesAdded = added
	fd.LinesRemoved = removed
	fd.DiffPreview = preview
}

// ReadObjectSafe reads a stored object by hash, returning nil on any error.
func ReadObjectSafe(projectRoot, hash string) []byte {
	if hash == "" {
		return nil
	}
	path := filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
	data, _ := os.ReadFile(path)
	return data
}

// SplitLines normalizes line endings and splits data into lines.
// A single trailing empty string produced by a terminal newline is trimmed so
// that a 3-line file ending with \n returns 3 elements, not 4.
// Genuine interior blank lines (two consecutive newlines) are preserved.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	// Normalize line endings so CRLF and LF lines compare equal.
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

const (
	// maxDiffFileLines guards the O(m*n) LCS table allocation.
	// Files larger than this show counts only, no inline diff.
	maxDiffFileLines = 2000
	// diffContextLines is the number of unchanged lines shown around each hunk.
	diffContextLines = 3
)

// EditOp classifies a single line in an edit sequence.
type EditOp int

const (
	// EditKeep means the line is unchanged between old and new.
	EditKeep EditOp = iota
	// EditAdd means the line was added in the new version.
	EditAdd
	// EditDelete means the line was removed from the old version.
	EditDelete
)

// EditLine is a single line in an edit sequence produced by ComputeEdits.
type EditLine struct {
	Op   EditOp
	Text string
}

// computeUnifiedDiff returns accurate added/removed line counts and a proper
// unified diff preview with hunk headers and context lines.
func computeUnifiedDiff(oldLines, newLines []string) (added, removed int, preview string) {
	lcs := lcsLength(oldLines, newLines)
	added = len(newLines) - lcs
	removed = len(oldLines) - lcs
	preview = buildUnifiedDiff(oldLines, newLines)
	return
}

// lcsLength computes the length of the Longest Common Subsequence using a
// space-efficient two-row DP table (O(n) space, O(m*n) time).
func lcsLength(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] > curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev = curr
	}
	return prev[len(b)]
}

// ComputeEdits returns an in-order edit sequence from oldLines to newLines
// using LCS backtracking (O(m*n) space). Returns nil when either file exceeds
// maxDiffFileLines.
func ComputeEdits(oldLines, newLines []string) []EditLine {
	m, n := len(oldLines), len(newLines)
	if m > maxDiffFileLines || n > maxDiffFileLines {
		return nil
	}
	// Full DP table for backtracking.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack to produce the edit sequence (built in reverse, then flipped).
	edits := make([]EditLine, 0, m+n)
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && oldLines[i-1] == newLines[j-1]:
			edits = append(edits, EditLine{EditKeep, oldLines[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			edits = append(edits, EditLine{EditAdd, newLines[j-1]})
			j--
		default:
			edits = append(edits, EditLine{EditDelete, oldLines[i-1]})
			i--
		}
	}
	for l, r := 0, len(edits)-1; l < r; l, r = l+1, r-1 {
		edits[l], edits[r] = edits[r], edits[l]
	}
	return edits
}

// posEdit is an edit line annotated with its 1-based old and new line numbers.
// oldLine is 0 for added lines; newLine is 0 for deleted lines.
type posEdit struct {
	op      EditOp
	text    string
	oldLine int
	newLine int
}

// buildUnifiedDiff produces a unified diff with @@ hunk headers and
// diffContextLines of context around each changed region.
// Returns an empty string when files exceed maxDiffFileLines.
func buildUnifiedDiff(oldLines, newLines []string) string {
	edits := ComputeEdits(oldLines, newLines)
	if edits == nil {
		return ""
	}

	// Annotate each edit with line numbers.
	positioned := make([]posEdit, len(edits))
	ol, nl := 1, 1
	for i, e := range edits {
		switch e.Op {
		case EditKeep:
			positioned[i] = posEdit{EditKeep, e.Text, ol, nl}
			ol++
			nl++
		case EditAdd:
			positioned[i] = posEdit{EditAdd, e.Text, 0, nl}
			nl++
		case EditDelete:
			positioned[i] = posEdit{EditDelete, e.Text, ol, 0}
			ol++
		}
	}

	// Mark which positions belong to a hunk (changed ± context).
	inHunk := make([]bool, len(positioned))
	for i, e := range positioned {
		if e.op != EditKeep {
			start := i - diffContextLines
			if start < 0 {
				start = 0
			}
			end := i + diffContextLines
			if end >= len(positioned) {
				end = len(positioned) - 1
			}
			for k := start; k <= end; k++ {
				inHunk[k] = true
			}
		}
	}

	var sb strings.Builder
	i := 0
	for i < len(positioned) {
		if !inHunk[i] {
			i++
			continue
		}
		hunkStart := i
		for i < len(positioned) && inHunk[i] {
			i++
		}
		slice := positioned[hunkStart:i]

		// Compute @@ header values.
		firstOld, firstNew := 0, 0
		oldCount, newCount := 0, 0
		for _, e := range slice {
			switch e.op {
			case EditKeep:
				if firstOld == 0 {
					firstOld = e.oldLine
				}
				if firstNew == 0 {
					firstNew = e.newLine
				}
				oldCount++
				newCount++
			case EditAdd:
				if firstNew == 0 {
					firstNew = e.newLine
				}
				newCount++
			case EditDelete:
				if firstOld == 0 {
					firstOld = e.oldLine
				}
				oldCount++
			}
		}

		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", firstOld, oldCount, firstNew, newCount)
		for _, e := range slice {
			switch e.op {
			case EditKeep:
				fmt.Fprintf(&sb, " %s\n", e.text)
			case EditAdd:
				fmt.Fprintf(&sb, "+%s\n", e.text)
			case EditDelete:
				fmt.Fprintf(&sb, "-%s\n", e.text)
			}
		}
	}
	return sb.String()
}

func sortDiffs(diffs []*FileDiff) {
	order := map[ChangeType]int{Added: 0, Modified: 1, Deleted: 2}
	for i := 1; i < len(diffs); i++ {
		for j := i; j > 0; j-- {
			a, b := diffs[j-1], diffs[j]
			if order[a.Type] > order[b.Type] || (order[a.Type] == order[b.Type] && a.Path > b.Path) {
				diffs[j-1], diffs[j] = diffs[j], diffs[j-1]
			}
		}
	}
}
