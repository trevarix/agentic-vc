// Package merge — conflict resolution helpers.
//
// ListConflicts scans the project root for files that still contain raw
// conflict markers (written by writeConflict during a merge).
//
// ResolveFile resolves a single conflicted file by writing the chosen
// version (ours/theirs/custom content) and removing the markers.
package merge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
)

const conflictMarker = "<<<<<<< main (ours)"

// ConflictFile describes a file that still contains unresolved conflict markers.
type ConflictFile struct {
	Path string `json:"path"`
}

// ListConflicts walks the project root and returns all files that contain
// unresolved conflict markers. These are the files written by writeConflict
// during the last merge. The branch name is accepted for future use (e.g.
// multi-merge disambiguation) but the scan is filesystem-based and does not
// require the branch to be active.
func ListConflicts(projectRoot string) ([]ConflictFile, error) {
	var conflicts []ConflictFile

	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip the .avc directory and hidden directories.
		if d.IsDir() {
			base := d.Name()
			if base == ".avc" || (len(base) > 0 && base[0] == '.') {
				return filepath.SkipDir
			}
			return nil
		}
		// Read the file and check for the marker.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // best-effort; skip unreadable files
		}
		if strings.Contains(string(data), conflictMarker) {
			rel, relErr := filepath.Rel(projectRoot, path)
			if relErr == nil {
				conflicts = append(conflicts, ConflictFile{Path: filepath.ToSlash(rel)})
			}
		}
		return nil
	})
	return conflicts, err
}

// ResolveFile writes the chosen version of a conflicted file to the project root,
// clearing the conflict markers.
//
// resolution must be one of:
//   - "ours"    — writes the main (ours) content from the merge record
//   - "theirs"  — writes the branch (theirs) content from the merge record
//   - "content" — writes the caller-supplied content verbatim
//
// branchName identifies whose merge record to consult for "ours"/"theirs".
func ResolveFile(projectRoot, branchName, filePath, resolution, content string) error {
	if resolution != "ours" && resolution != "theirs" && resolution != "content" {
		return fmt.Errorf("resolution must be 'ours', 'theirs', or 'content'")
	}
	if resolution == "content" && content == "" {
		return fmt.Errorf("content must be provided when resolution is 'content'")
	}

	dest := filepath.Join(projectRoot, filepath.FromSlash(filePath))

	if resolution == "content" {
		return fileutil.WriteFile(dest, []byte(content))
	}

	// Look up the file's hashes from the last merge record for this branch.
	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return err
	}

	agentBranch, err := store.GetBranchByName(proj.ID, branchName)
	if err != nil {
		return fmt.Errorf("branch '%s' not found", branchName)
	}

	m, err := store.GetLastMerge(agentBranch.ID)
	if err != nil {
		return fmt.Errorf("no merge record found for branch '%s'", branchName)
	}
	if m.Status != "conflicts" && m.Status != "in_progress" {
		return fmt.Errorf("last merge for branch '%s' has status '%s' — no conflicts to resolve", branchName, m.Status)
	}

	mf, err := store.GetMergeFileByPath(m.ID, filePath)
	if err != nil {
		return fmt.Errorf("file '%s' not found in merge record: %w", filePath, err)
	}

	var hash string
	switch resolution {
	case "ours":
		hash = mf.MainHash
	case "theirs":
		hash = mf.BranchHash
	}

	if hash == "" {
		// File was absent on the chosen side — delete it.
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove file: %w", err)
		}
		return nil
	}

	data, err := restore.ReadObject(projectRoot, hash)
	if err != nil {
		return fmt.Errorf("read object %s: %w", hash, err)
	}
	return fileutil.WriteFile(dest, data)
}
