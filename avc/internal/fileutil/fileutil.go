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

// ignorePattern is one compiled line from .avcignore, following gitignore
// syntax and precedence rules.
type ignorePattern struct {
	segments []string // pattern split on "/"; "**" segments match zero or more path segments
	dirOnly  bool      // true if the pattern ended in "/" — only matches directories
	negate   bool      // true if the pattern started with "!" — un-ignores a prior match
}

// compilePattern parses one non-comment, non-blank .avcignore line.
//
// A pattern containing no "/" (other than an optional trailing one, e.g.
// "node_modules" or "build/") is unanchored — it matches at any depth, not
// just at the project root. This is implemented by implicitly prefixing such
// patterns with "**/", which also gives every pattern uniform "**" handling.
func compilePattern(raw string) ignorePattern {
	negate := strings.HasPrefix(raw, "!")
	if negate {
		raw = raw[1:]
	}
	dirOnly := strings.HasSuffix(raw, "/")
	body := raw
	if dirOnly {
		body = strings.TrimSuffix(body, "/")
	}

	anchored := strings.Contains(body, "/")
	body = strings.TrimPrefix(body, "/")
	if !anchored {
		body = "**/" + body
	}

	return ignorePattern{segments: strings.Split(body, "/"), dirOnly: dirOnly, negate: negate}
}

// IgnoreRules holds compiled patterns from .avcignore, in file order (later
// patterns — including "!" negations — take precedence over earlier ones,
// matching gitignore semantics).
type IgnoreRules struct {
	patterns []ignorePattern
}

// LoadIgnoreRules reads .avcignore from projectRoot. If the file doesn't exist,
// returns an empty rule set (nothing ignored beyond .avc/).
func LoadIgnoreRules(projectRoot string) (*IgnoreRules, error) {
	return LoadIgnoreRulesFrom(filepath.Join(projectRoot, ".avcignore"))
}

// LoadIgnoreRulesFrom reads ignore rules from an explicit file path.
func LoadIgnoreRulesFrom(path string) (*IgnoreRules, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &IgnoreRules{}, nil
	}
	if err != nil {
		return nil, err
	}

	var patterns []ignorePattern
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, compilePattern(line))
		}
	}
	return &IgnoreRules{patterns: patterns}, nil
}

// Matches returns true if the given slash-separated relative file path
// matches the ignore rules. dirOnly patterns (ending in "/") never match
// files.
func (r *IgnoreRules) Matches(rel string) bool {
	return r.matchAny(rel, false)
}

// MatchesDir returns true if the given slash-separated relative directory
// path matches the ignore rules — dirOnly patterns apply here, in addition
// to every pattern that also matches files.
func (r *IgnoreRules) MatchesDir(rel string) bool {
	return r.matchAny(rel, true)
}

// matchAny applies every pattern in file order and returns the outcome of
// the last one that matched — this is what makes a later "!pattern" able to
// un-ignore something an earlier broader pattern excluded.
func (r *IgnoreRules) matchAny(rel string, isDir bool) bool {
	segments := strings.Split(rel, "/")
	matched := false
	for _, p := range r.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if matchSegments(p.segments, segments) {
			matched = !p.negate
		}
	}
	return matched
}

// matchSegments recursively matches pattern segments against path segments.
// A "**" segment matches zero or more path segments; every other segment is
// matched against exactly one path segment via filepath.Match (so "*" and
// "?" never cross a "/" boundary).
func matchSegments(pat, path []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], path) {
			return true
		}
		return len(path) > 0 && matchSegments(pat, path[1:])
	}
	if len(path) == 0 {
		return false
	}
	if ok, err := filepath.Match(pat[0], path[0]); err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], path[1:])
}
