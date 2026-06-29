// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fileutil provides file hashing, directory walking, and I/O helpers.
package fileutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HashFile returns the SHA256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReadFile reads and returns the full contents of a file.
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadAndHash reads the file at path once and returns its contents and SHA256
// hex digest. Use this in preference to calling HashFile + ReadFile separately.
func ReadAndHash(path string) (data []byte, hash string, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// WriteFile writes data to path, creating parent directories as needed.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// WalkProject returns all tracked file paths under root, respecting ignore rules.
func WalkProject(root string, ignore *IgnoreRules) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Always skip these directories regardless of .avcignore.
		if d.IsDir() {
			switch d.Name() {
			case ".avc", ".git", ".hg", ".svn", ".bzr":
				return filepath.SkipDir
			}
		}

		if d.IsDir() {
			if ignore.MatchesDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if !ignore.Matches(rel) {
			paths = append(paths, path)
		}
		return nil
	})

	return paths, err
}

// IgnoreRules holds compiled patterns from .avcignore.
type IgnoreRules struct {
	patterns []string
}

// LoadIgnoreRules reads .avcignore from projectRoot. If the file doesn't exist,
// returns an empty rule set (nothing ignored beyond .avc/).
func LoadIgnoreRules(projectRoot string) (*IgnoreRules, error) {
	path := filepath.Join(projectRoot, ".avcignore")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &IgnoreRules{}, nil
	}
	if err != nil {
		return nil, err
	}

	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return &IgnoreRules{patterns: patterns}, nil
}

// Matches returns true if the given slash-separated relative path matches any
// ignore pattern.
func (r *IgnoreRules) Matches(rel string) bool {
	for _, pat := range r.patterns {
		if matchPattern(pat, rel) {
			return true
		}
	}
	return false
}

// MatchesDir returns true if a directory should be skipped entirely.
func (r *IgnoreRules) MatchesDir(rel string) bool {
	return r.Matches(rel) || r.Matches(rel+"/")
}

// matchPattern is a simple glob matcher supporting * and ** wildcards.
func matchPattern(pattern, path string) bool {
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}
	// Also match if any path component matches the pattern (e.g. "node_modules").
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if m, _ := filepath.Match(pattern, part); m {
			return true
		}
	}
	return false
}
