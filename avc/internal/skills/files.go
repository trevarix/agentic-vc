// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// File action results. A skip carries its reason after a colon; see fileAction.
const (
	actionCreated         = "created"
	actionUpdated         = "updated"
	actionRemoved         = "removed"
	skipUpToDate          = "skipped:up to date"
	skipAlreadyExists     = "skipped:already exists"
	skipUserOwned         = "skipped:not AVC-managed — left untouched"
	skipAlreadyConfigured = "skipped:already configured"
)

// writeManagedFile writes a whole file AVC owns.
//
// A missing file is created. An existing file is rewritten only when it is
// AVC's — it carries managedFileMarker, or isLegacy recognises it as written
// by an older AVC version — so files from before a rules fix get the fix on
// the next `avc init --skills`. A file the user wrote is never overwritten.
// isLegacy may be nil.
func writeManagedFile(path, content string, isLegacy func(string) bool) (string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return actionCreated, writeFileMkdir(path, content)
	}
	if err != nil {
		return "", err
	}
	current := string(existing)
	if current == content {
		return skipUpToDate, nil
	}
	if !strings.Contains(current, managedFileMarker) && (isLegacy == nil || !isLegacy(current)) {
		return skipUserOwned, nil
	}
	return actionUpdated, writeFileMkdir(path, content)
}

// upsertBlock writes the AVC section into a file that may hold other content.
//
// The section between blockStartMarker and blockEndMarker is replaced when it
// differs; a file without one gets the section appended; a missing file is
// created. Content outside the markers is never touched.
func upsertBlock(path, block string) (string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return actionCreated, writeFileMkdir(path, block)
	}
	if err != nil {
		return "", err
	}
	current := string(existing)

	if start, end, ok := findBlock(current); ok {
		if current[start:end] == strings.TrimSuffix(block, "\n") {
			return skipUpToDate, nil
		}
		return actionUpdated, os.WriteFile(path, []byte(current[:start]+strings.TrimSuffix(block, "\n")+current[end:]), 0644)
	}

	sep := "\n"
	if strings.HasSuffix(current, "\n") || current == "" {
		sep = ""
	}
	if current != "" {
		sep += "\n"
	}
	return actionUpdated, os.WriteFile(path, []byte(current+sep+block), 0644)
}

// removeBlock deletes the AVC section from path, and deletes the file when
// nothing but whitespace remains. Returns "" when there was nothing to remove.
func removeBlock(path string) (string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	current := string(existing)
	start, end, ok := findBlock(current)
	if !ok {
		return "", nil
	}
	rest := strings.TrimSpace(current[:start] + current[end:])
	if rest == "" {
		return actionRemoved, os.Remove(path)
	}
	return actionUpdated, os.WriteFile(path, []byte(rest+"\n"), 0644)
}

// findBlock locates the AVC section in content, returning the byte range from
// its start marker to the end of its end marker.
//
// Older Windsurf rules blocks had no start marker: they opened with the
// "# AVC — Agentic Version Control" heading and closed with the end marker.
// Those are found too, so they can be refreshed or removed.
func findBlock(content string) (start, end int, ok bool) {
	endIdx := strings.Index(content, blockEndMarker)
	if endIdx < 0 {
		return 0, 0, false
	}
	end = endIdx + len(blockEndMarker)

	if s := strings.LastIndex(content[:endIdx], blockStartMarker); s >= 0 {
		return s, end, true
	}
	if s := strings.LastIndex(content[:endIdx], "# "+rulesTitle); s >= 0 {
		return s, end, true
	}
	return 0, 0, false
}

// writeFileIfAbsent writes content only if path does not exist.
func writeFileIfAbsent(path, content string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return skipAlreadyExists, nil
	}
	return actionCreated, writeFileMkdir(path, content)
}

// writeFileMkdir writes content to path, creating parent directories.
func writeFileMkdir(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
