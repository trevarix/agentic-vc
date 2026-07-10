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

	dst := filepath.Join(s.projectRoot, ".avc", trashDir, s.opID, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create trash dir: %w", err)
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
