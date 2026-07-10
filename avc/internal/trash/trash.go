// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package trash quarantines files that a destructive AVC operation (restore,
// merge) would otherwise permanently delete. Nothing routed through this
// package is ever unrecoverable — it is moved to .avc/trash/<opID>/ instead
// of being removed, and can be listed or restored later.
package trash

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const trashDir = "trash"

// metaFile, inside each op directory, records the directory quarantined
// files were moved out of (project root for main, the workspace for a
// branch) so `avc trash restore` can put them back where they came from.
const metaFile = ".avc-trash-meta"

// Session groups every file quarantined by one operation (e.g. one restore)
// under a single timestamped directory, so the whole operation can be
// inspected or emptied as a unit.
type Session struct {
	projectRoot string
	opID        string
	created     bool
}

// NewSession starts a trash session for one destructive operation. kind is a
// short human-readable label (e.g. "restore", "merge") embedded in the
// directory name. The directory itself is created lazily on first Move, so a
// clean operation that never quarantines anything leaves no trace.
func NewSession(projectRoot, kind string) *Session {
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	opID := fmt.Sprintf("%s-%s-%s",
		time.Now().Format("2006-01-02T15-04-05"), kind, hex.EncodeToString(suffix))
	return &Session{projectRoot: projectRoot, opID: opID}
}

// Move relocates targetDir/rel into this session's trash directory,
// preserving the relative path underneath it. If the source file does not
// exist, Move is a no-op. Trash failures are the caller's to decide how to
// handle — quarantining is a best-effort defense in depth, and callers
// should never let a trash error block the operation it protects.
func (s *Session) Move(targetDir, rel string) error {
	src := filepath.Join(targetDir, filepath.FromSlash(rel))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	opDir := filepath.Join(s.projectRoot, ".avc", trashDir, s.opID)
	dst := filepath.Join(opDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create trash dir: %w", err)
	}
	if !s.created {
		// First quarantined file — record where files came from so
		// `avc trash restore` can put them back. Best-effort.
		_ = os.WriteFile(filepath.Join(opDir, metaFile), []byte(targetDir), 0644)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move %s to trash: %w", rel, err)
	}
	s.created = true
	return nil
}

// OpID returns the session's directory name, or "" if nothing was quarantined.
func (s *Session) OpID() string {
	if !s.created {
		return ""
	}
	return s.opID
}

// Entry describes one quarantined operation.
type Entry struct {
	OpID      string    `json:"op_id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	Files     []string  `json:"files"`
}

// List returns trash entries grouped by opID, newest first.
func List(projectRoot string) ([]Entry, error) {
	root := filepath.Join(projectRoot, ".avc", trashDir)
	dirEntries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		entry, err := readEntry(root, de.Name())
		if err != nil {
			continue // skip malformed/unreadable entries rather than failing the whole list
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CreatedAt.After(entries[j].CreatedAt) })
	return entries, nil
}

func readEntry(root, opID string) (Entry, error) {
	info, err := os.Stat(filepath.Join(root, opID))
	if err != nil {
		return Entry{}, err
	}

	var files []string
	err = filepath.WalkDir(filepath.Join(root, opID), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(path) == metaFile {
			return nil // internal bookkeeping, not a quarantined file
		}
		rel, relErr := filepath.Rel(filepath.Join(root, opID), path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Entry{}, err
	}

	return Entry{
		OpID:      opID,
		Kind:      parseKind(opID),
		CreatedAt: info.ModTime(),
		Files:     files,
	}, nil
}

// parseKind extracts the "restore"/"merge" label from an opID formatted as
// "<timestamp>-<kind>-<random>". Falls back to "" if the format is unexpected.
func parseKind(opID string) string {
	parts := strings.Split(opID, "-")
	// Timestamp is "2006-01-02T15-04-05" -> 5 hyphen-separated parts, then kind, then random.
	if len(parts) < 7 {
		return ""
	}
	return parts[5]
}

// Restore moves quarantined files from a trash entry back to where they came
// from (the directory recorded when they were quarantined; project root when
// no record exists). Pass path="" to restore every file in the entry, or a
// specific relative path to restore just that file. Existing files at the
// destination are never overwritten — they are reported as skipped instead,
// since the file on disk may hold newer work.
func Restore(projectRoot, opID, path string) (restored, skipped []string, err error) {
	opDir := filepath.Join(projectRoot, ".avc", trashDir, opID)
	if _, statErr := os.Stat(opDir); os.IsNotExist(statErr) {
		return nil, nil, fmt.Errorf("trash entry '%s' not found", opID)
	}

	targetDir := projectRoot
	if meta, metaErr := os.ReadFile(filepath.Join(opDir, metaFile)); metaErr == nil {
		if dir := strings.TrimSpace(string(meta)); dir != "" {
			targetDir = dir
		}
	}

	entry, err := readEntry(filepath.Join(projectRoot, ".avc", trashDir), opID)
	if err != nil {
		return nil, nil, err
	}

	for _, rel := range entry.Files {
		if path != "" && rel != path {
			continue
		}
		src := filepath.Join(opDir, filepath.FromSlash(rel))
		dst := filepath.Join(targetDir, filepath.FromSlash(rel))

		if _, statErr := os.Stat(dst); statErr == nil {
			skipped = append(skipped, rel) // never clobber a live file
			continue
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0755); mkErr != nil {
			return restored, skipped, fmt.Errorf("create parent for %s: %w", rel, mkErr)
		}
		if mvErr := os.Rename(src, dst); mvErr != nil {
			return restored, skipped, fmt.Errorf("restore %s from trash: %w", rel, mvErr)
		}
		restored = append(restored, rel)
	}
	if path != "" && len(restored) == 0 && len(skipped) == 0 {
		return nil, nil, fmt.Errorf("file '%s' not found in trash entry '%s'", path, opID)
	}

	// If everything was restored, remove the now-empty entry (best-effort).
	if remaining, listErr := readEntry(filepath.Join(projectRoot, ".avc", trashDir), opID); listErr == nil && len(remaining.Files) == 0 {
		_ = os.RemoveAll(opDir)
	}
	return restored, skipped, nil
}

// Empty removes trash entries older than olderThan. Pass 0 to remove all
// entries regardless of age. Returns the number of operation directories removed.
func Empty(projectRoot string, olderThan time.Duration) (int, error) {
	root := filepath.Join(projectRoot, ".avc", trashDir)
	dirEntries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		path := filepath.Join(root, de.Name())
		if olderThan > 0 {
			info, statErr := os.Stat(path)
			if statErr != nil || info.ModTime().After(cutoff) {
				continue
			}
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Errorf("remove trash entry %s: %w", de.Name(), err)
		}
		removed++
	}
	return removed, nil
}
