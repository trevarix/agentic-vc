// Package diff computes file-level differences between two snapshots.
package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-version-ctl/avc/internal/db"
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
	oldData := readObjectSafe(projectRoot, fd.OldHash)
	newData := readObjectSafe(projectRoot, fd.NewHash)

	added, removed, preview := computeUnifiedDiff(splitLines(oldData), splitLines(newData))
	fd.LinesAdded = added
	fd.LinesRemoved = removed
	fd.DiffPreview = preview
}

func readObjectSafe(projectRoot, hash string) []byte {
	if hash == "" {
		return nil
	}
	path := filepath.Join(projectRoot, ".avc", "objects", hash[:2], hash[2:])
	data, _ := os.ReadFile(path)
	return data
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	// Normalize line endings so CRLF and LF lines compare equal.
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}

// computeUnifiedDiff returns accurate added/removed line counts and a short
// preview using the Longest Common Subsequence algorithm. This correctly
// handles duplicate lines (e.g. blank lines, repeated patterns) that the
// previous set-based approach miscounted.
func computeUnifiedDiff(oldLines, newLines []string) (added, removed int, preview string) {
	lcs := lcsLength(oldLines, newLines)
	added = len(newLines) - lcs
	removed = len(oldLines) - lcs
	preview = buildPreview(oldLines, newLines)
	return
}

// lcsLength computes the length of the Longest Common Subsequence of two line
// slices using a space-efficient two-row DP table (O(n) space, O(m*n) time).
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

// buildPreview produces a short unified-style excerpt of the changes.
// It uses multiset counting so duplicate lines are handled correctly.
func buildPreview(oldLines, newLines []string) string {
	const maxPreviewLines = 10

	oldCounts := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		oldCounts[l]++
	}
	newCounts := make(map[string]int, len(newLines))
	for _, l := range newLines {
		newCounts[l]++
	}

	var preview []string
	for _, l := range newLines {
		if oldCounts[l] > 0 {
			oldCounts[l]--
		} else if len(preview) < maxPreviewLines {
			preview = append(preview, "+"+l)
		}
	}
	for _, l := range oldLines {
		if newCounts[l] > 0 {
			newCounts[l]--
		} else if len(preview) < maxPreviewLines {
			preview = append(preview, "-"+l)
		}
	}
	return strings.Join(preview, "\n")
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
