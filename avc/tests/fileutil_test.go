package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
)

// TestFileutil_HashFile_Consistent verifies that hashing the same file twice
// returns the same digest.
func TestFileutil_HashFile_Consistent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "file.go", "package main\n")

	path := filepath.Join(dir, "file.go")
	h1, err := fileutil.HashFile(path)
	if err != nil {
		t.Fatalf("HashFile first call: %v", err)
	}
	h2, err := fileutil.HashFile(path)
	if err != nil {
		t.Fatalf("HashFile second call: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash not stable: %q != %q", h1, h2)
	}
}

// TestFileutil_HashFile_CorrectSHA256 verifies that HashFile returns the
// correct SHA256 hex digest for a known input.
func TestFileutil_HashFile_CorrectSHA256(t *testing.T) {
	dir := t.TempDir()
	content := "hello, avc\n"
	writeFile(t, dir, "known.txt", content)

	expected := sha256hex(content)
	got, err := fileutil.HashFile(filepath.Join(dir, "known.txt"))
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if got != expected {
		t.Errorf("hash = %q, want %q", got, expected)
	}
}

// TestFileutil_HashFile_DifferentContentDifferentHash verifies that two files
// with different content produce different digests.
func TestFileutil_HashFile_DifferentContentDifferentHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "content A")
	writeFile(t, dir, "b.txt", "content B")

	hA, _ := fileutil.HashFile(filepath.Join(dir, "a.txt"))
	hB, _ := fileutil.HashFile(filepath.Join(dir, "b.txt"))
	if hA == hB {
		t.Error("different content must produce different hashes")
	}
}

// TestFileutil_HashFile_ErrorOnMissingFile verifies that hashing a non-existent
// file returns an error.
func TestFileutil_HashFile_ErrorOnMissingFile(t *testing.T) {
	_, err := fileutil.HashFile("/tmp/__no_such_file_avc__.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestFileutil_ReadAndHash_ConsistentWithHashFile verifies that ReadAndHash
// returns the same hash as HashFile for the same file.
func TestFileutil_ReadAndHash_ConsistentWithHashFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "file.go", "package main\nfunc main() {}\n")

	path := filepath.Join(dir, "file.go")
	_, hash1, err := fileutil.ReadAndHash(path)
	if err != nil {
		t.Fatalf("ReadAndHash: %v", err)
	}
	hash2, err := fileutil.HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("ReadAndHash hash %q != HashFile hash %q", hash1, hash2)
	}
}

// TestFileutil_ReadAndHash_ReturnsCorrectContent verifies that ReadAndHash
// returns the file's full content alongside the hash.
func TestFileutil_ReadAndHash_ReturnsCorrectContent(t *testing.T) {
	dir := t.TempDir()
	const content = "package main\n"
	writeFile(t, dir, "file.go", content)

	data, _, err := fileutil.ReadAndHash(filepath.Join(dir, "file.go"))
	if err != nil {
		t.Fatalf("ReadAndHash: %v", err)
	}
	if string(data) != content {
		t.Errorf("data = %q, want %q", string(data), content)
	}
}

// TestFileutil_WriteFile_CreatesParentDirectories verifies that WriteFile
// creates intermediate directories if they do not exist.
func TestFileutil_WriteFile_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "a", "b", "c", "file.go")

	if err := fileutil.WriteFile(dest, []byte("content")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("data = %q, want %q", string(data), "content")
	}
}

// TestFileutil_WalkProject_ExcludesAVCDirectory verifies that WalkProject
// never returns files inside .avc/.
func TestFileutil_WalkProject_ExcludesAVCDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, ".avc/internal.db", "db data")

	rules, err := fileutil.LoadIgnoreRules(dir)
	if err != nil {
		t.Fatalf("LoadIgnoreRules: %v", err)
	}

	paths, err := fileutil.WalkProject(dir, rules)
	if err != nil {
		t.Fatalf("WalkProject: %v", err)
	}

	for _, p := range paths {
		if len(p) >= 4 && p[:4] == ".avc" {
			t.Errorf("WalkProject returned .avc file: %q", p)
		}
	}
}

// TestFileutil_WalkProject_ExcludesGitDirectory verifies that WalkProject
// never returns files inside .git/.
func TestFileutil_WalkProject_ExcludesGitDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# hello")
	writeFile(t, dir, ".git/config", "git config")

	rules, err := fileutil.LoadIgnoreRules(dir)
	if err != nil {
		t.Fatalf("LoadIgnoreRules: %v", err)
	}

	paths, err := fileutil.WalkProject(dir, rules)
	if err != nil {
		t.Fatalf("WalkProject: %v", err)
	}

	for _, p := range paths {
		if len(p) >= 4 && p[:4] == ".git" {
			t.Errorf("WalkProject returned .git file: %q", p)
		}
	}
}

// TestFileutil_WalkProject_ReturnsTrackedFiles verifies that WalkProject
// returns absolute paths for tracked files (callers derive relative paths
// with filepath.Rel), and that the expected files are present.
func TestFileutil_WalkProject_ReturnsTrackedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/app.go", "package app\n")
	writeFile(t, dir, "README.md", "# readme")

	rules, err := fileutil.LoadIgnoreRules(dir)
	if err != nil {
		t.Fatalf("LoadIgnoreRules: %v", err)
	}

	paths, err := fileutil.WalkProject(dir, rules)
	if err != nil {
		t.Fatalf("WalkProject: %v", err)
	}

	// Convert to relative paths for platform-independent assertions.
	relPaths := make(map[string]bool)
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatalf("Rel: %v", err)
		}
		relPaths[filepath.ToSlash(rel)] = true
	}

	if !relPaths["README.md"] {
		t.Error("expected README.md in walk results")
	}
	if !relPaths["src/app.go"] {
		t.Error("expected src/app.go in walk results")
	}
}

// TestFileutil_LoadIgnoreRules_EmptyRulesMatchNothing verifies that empty
// ignore rules do not exclude any file.
func TestFileutil_LoadIgnoreRules_EmptyRulesMatchNothing(t *testing.T) {
	dir := t.TempDir()
	rules, err := fileutil.LoadIgnoreRules(dir)
	if err != nil {
		t.Fatalf("LoadIgnoreRules: %v", err)
	}

	if rules.Matches("any/file.go") {
		t.Error("empty rules should not match any file")
	}
}

// TestFileutil_LoadIgnoreRules_IgnoresMatchedPaths verifies that patterns in
// .avcignore are respected.
func TestFileutil_LoadIgnoreRules_IgnoresMatchedPaths(t *testing.T) {
	dir := t.TempDir()
	// Write an .avcignore that excludes *.log
	if err := os.WriteFile(filepath.Join(dir, ".avcignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatalf("write .avcignore: %v", err)
	}

	rules, err := fileutil.LoadIgnoreRules(dir)
	if err != nil {
		t.Fatalf("LoadIgnoreRules: %v", err)
	}

	if !rules.Matches("debug.log") {
		t.Error("expected *.log pattern to match debug.log")
	}
	if rules.Matches("main.go") {
		t.Error("expected *.log pattern not to match main.go")
	}
}

// sha256hex returns the SHA-256 hex digest of a string.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
