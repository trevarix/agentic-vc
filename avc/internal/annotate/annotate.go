// Package annotate traces line origins across snapshots for a single file.
package annotate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
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

// Annotate traces line origins for filePath across all snapshots.
// filePath must be a slash-separated relative path within the project.
func Annotate(projectRoot, filePath string) (*AnnotateResult, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	snapshots, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}

	if len(snapshots) == 0 {
		return &AnnotateResult{FilePath: filePath}, nil
	}

	// Sort snapshots oldest-first for forward traversal.
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})

	// Collect (snapshot, hash) pairs for the file across history.
	type fileVersion struct {
		snap *db.Snapshot
		hash string
	}

	var versions []fileVersion
	for _, snap := range snapshots {
		files, err := store.GetSnapshotFiles(snap.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.RelativePath == filePath {
				versions = append(versions, fileVersion{snap: snap, hash: f.FileHash})
				break
			}
		}
	}

	if len(versions) == 0 {
		// File was never tracked. Try to annotate from current disk.
		return annotateCurrentOnly(projectRoot, filePath)
	}

	// Start with the first version: all lines belong to the first snapshot.
	firstData := diff.ReadObjectSafe(projectRoot, versions[0].hash)
	firstLines := diff.SplitLines(firstData)

	// origins[i] tracks which snapshot introduced line i of the current version.
	origins := make([]*db.Snapshot, len(firstLines))
	for i := range origins {
		origins[i] = versions[0].snap
	}

	// Walk forward through versions, updating origins when the file changed.
	for v := 1; v < len(versions); v++ {
		if versions[v].hash == versions[v-1].hash {
			continue // identical content, skip
		}

		oldData := diff.ReadObjectSafe(projectRoot, versions[v-1].hash)
		newData := diff.ReadObjectSafe(projectRoot, versions[v].hash)
		oldLines := diff.SplitLines(oldData)
		newLines := diff.SplitLines(newData)

		edits := diff.ComputeEdits(oldLines, newLines)
		if edits == nil {
			// File too large for LCS. Attribute all lines to this snapshot.
			origins = make([]*db.Snapshot, len(newLines))
			for i := range origins {
				origins[i] = versions[v].snap
			}
			continue
		}

		newOrigins := make([]*db.Snapshot, 0, len(newLines))
		oldIdx := 0
		for _, e := range edits {
			switch e.Op {
			case diff.EditKeep:
				if oldIdx < len(origins) {
					newOrigins = append(newOrigins, origins[oldIdx])
				} else {
					newOrigins = append(newOrigins, versions[v].snap)
				}
				oldIdx++
			case diff.EditAdd:
				newOrigins = append(newOrigins, versions[v].snap)
			case diff.EditDelete:
				oldIdx++
			}
		}
		origins = newOrigins
	}

	// Optionally compare latest snapshot with current disk file.
	latestHash := versions[len(versions)-1].hash
	absPath := filepath.Join(projectRoot, filepath.FromSlash(filePath))
	currentData, err := os.ReadFile(absPath)
	if err == nil {
		currentLines := diff.SplitLines(currentData)
		storedData := diff.ReadObjectSafe(projectRoot, latestHash)
		storedLines := diff.SplitLines(storedData)

		if !linesEqual(storedLines, currentLines) {
			edits := diff.ComputeEdits(storedLines, currentLines)
			if edits != nil {
				newOrigins := make([]*db.Snapshot, 0, len(currentLines))
				oldIdx := 0
				// Use the latest snapshot as the "current" attribution.
				latestSnap := versions[len(versions)-1].snap
				for _, e := range edits {
					switch e.Op {
					case diff.EditKeep:
						if oldIdx < len(origins) {
							newOrigins = append(newOrigins, origins[oldIdx])
						} else {
							newOrigins = append(newOrigins, latestSnap)
						}
						oldIdx++
					case diff.EditAdd:
						newOrigins = append(newOrigins, latestSnap)
					case diff.EditDelete:
						oldIdx++
					}
				}
				origins = newOrigins
			}
		}
	}

	// Build result.
	result := &AnnotateResult{
		FilePath:   filePath,
		TotalLines: len(origins),
		Lines:      make([]*LineAnnotation, len(origins)),
	}
	for i, snap := range origins {
		result.Lines[i] = &LineAnnotation{
			Line:       i + 1,
			SnapshotID: snap.ID,
			Label:      snap.Label,
			AgentName:  snap.AgentName,
			Timestamp:  snap.Timestamp,
		}
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
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

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
