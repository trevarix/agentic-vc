package tests

import (
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/annotate"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
)

// TestAnnotate_SingleSnapshot_AllLinesToFirstSnapshot verifies that when only
// one snapshot exists, every line is attributed to that snapshot.
func TestAnnotate_SingleSnapshot_AllLinesToFirstSnapshot(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "line1\nline2\nline3\n")

	snap, err := snapshot.Create(projectRoot, "initial", "agent-a", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := annotate.Annotate(projectRoot, "app.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	if result.TotalLines == 0 {
		t.Fatal("expected non-zero line count")
	}
	for _, line := range result.Lines {
		if line.SnapshotID != snap.ID {
			t.Errorf("line %d attributed to %q, want %q", line.Line, line.SnapshotID, snap.ID)
		}
	}
}

// TestAnnotate_AddedLines_AttributedToLaterSnapshot verifies that lines added
// in a second snapshot are attributed to that snapshot, while original lines
// retain their first-snapshot attribution.
func TestAnnotate_AddedLines_AttributedToLaterSnapshot(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "app.go", "original line\n")

	snap1, err := snapshot.Create(projectRoot, "v1", "agent-a", "", "", "")
	if err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}

	// Add a new line.
	writeFile(t, projectRoot, "app.go", "original line\nnew line added\n")

	snap2, err := snapshot.Create(projectRoot, "v2", "agent-b", "", "", "")
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}

	result, err := annotate.Annotate(projectRoot, "app.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	if len(result.Lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(result.Lines))
	}

	// First line should be from snap1.
	if result.Lines[0].SnapshotID != snap1.ID {
		t.Errorf("line 1 attributed to %q, want snap1 %q", result.Lines[0].SnapshotID, snap1.ID)
	}

	// Second line should be from snap2.
	if result.Lines[1].SnapshotID != snap2.ID {
		t.Errorf("line 2 attributed to %q, want snap2 %q", result.Lines[1].SnapshotID, snap2.ID)
	}
}

// TestAnnotate_EmptyProject_ReturnsEmptyLines verifies that a project with no
// snapshots for a file returns an empty (or untracked) result.
func TestAnnotate_EmptyProject_ReturnsEmptyLines(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Annotate a file that has never been snapshotted (but write it on disk for fallback).
	writeFile(t, projectRoot, "untracked.go", "some content\n")

	result, err := annotate.Annotate(projectRoot, "untracked.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	// Either returns an untracked result or empty — either is acceptable.
	// Verify the result is well-formed.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FilePath != "untracked.go" {
		t.Errorf("FilePath = %q, want %q", result.FilePath, "untracked.go")
	}
}

// TestAnnotate_FilePath_ReturnsCorrectPath verifies that the returned FilePath
// matches what was requested.
func TestAnnotate_FilePath_ReturnsCorrectPath(t *testing.T) {
	projectRoot := setupTestProject(t)
	writeFile(t, projectRoot, "src/service.go", "package service\n")

	_, err := snapshot.Create(projectRoot, "init", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := annotate.Annotate(projectRoot, "src/service.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	if result.FilePath != "src/service.go" {
		t.Errorf("FilePath = %q, want %q", result.FilePath, "src/service.go")
	}
}

// TestAnnotate_FileNotInAnySnapshot_FallsBackToDisk verifies that annotating
// a file not in any snapshot falls back to the current disk state, labeling
// all lines as "(untracked)".
func TestAnnotate_FileNotInAnySnapshot_FallsBackToDisk(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Take a snapshot of a different file, not the one we'll annotate.
	writeFile(t, projectRoot, "other.go", "package main\n")
	_, err := snapshot.Create(projectRoot, "snap-other", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Write the file to annotate only after the snapshot.
	writeFile(t, projectRoot, "late.go", "line a\nline b\n")

	result, err := annotate.Annotate(projectRoot, "late.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	for _, line := range result.Lines {
		if line.Label != "(untracked)" {
			t.Errorf("line %d label = %q, want %q", line.Line, line.Label, "(untracked)")
		}
	}
}

// TestAnnotate_NonExistentFile_ReturnsError verifies that annotating a file
// that neither exists on disk nor in any snapshot returns an error.
// At least one snapshot of another file must exist first so the annotate
// function passes the early-exit guard and reaches the "file not found" path.
func TestAnnotate_NonExistentFile_ReturnsError(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Snapshot a different file so the annotate function has history to search.
	writeFile(t, projectRoot, "other.go", "package main\n")
	if _, err := snapshot.Create(projectRoot, "snap", "", "", "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	_, err := annotate.Annotate(projectRoot, "does_not_exist.go")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// TestAnnotate_TotalLines_MatchesFileContent verifies that TotalLines matches
// the number of lines in the current file content.
func TestAnnotate_TotalLines_MatchesFileContent(t *testing.T) {
	projectRoot := setupTestProject(t)
	content := "alpha\nbeta\ngamma\ndelta\n"
	writeFile(t, projectRoot, "file.go", content)

	_, err := snapshot.Create(projectRoot, "snap", "", "", "", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := annotate.Annotate(projectRoot, "file.go")
	if err != nil {
		t.Fatalf("annotate: %v", err)
	}

	if result.TotalLines != len(result.Lines) {
		t.Errorf("TotalLines=%d does not match len(Lines)=%d", result.TotalLines, len(result.Lines))
	}
}

func ann(line int, snap string) *annotate.LineAnnotation {
	return &annotate.LineAnnotation{Line: line, SnapshotID: snap}
}

// TestCollapseBlocks_GroupsContiguousSameSnapshot verifies the blame-style
// grouping used by both the CLI output and the VSCode annotations.
func TestCollapseBlocks_GroupsContiguousSameSnapshot(t *testing.T) {
	blocks := annotate.CollapseBlocks([]*annotate.LineAnnotation{
		ann(1, "A"), ann(2, "A"), ann(3, "A"),
		ann(4, "B"),
		ann(5, "A"), ann(6, "A"), // later run of A is its own block
	})
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	want := [][3]interface{}{{1, 3, "A"}, {4, 4, "B"}, {5, 6, "A"}}
	for i, w := range want {
		if blocks[i].Start != w[0] || blocks[i].End != w[1] || blocks[i].Line.SnapshotID != w[2] {
			t.Errorf("block %d = {%d,%d,%q}, want %v", i, blocks[i].Start, blocks[i].End, blocks[i].Line.SnapshotID, w)
		}
	}
}

func TestCollapseBlocks_NonContiguousBreaksBlock(t *testing.T) {
	blocks := annotate.CollapseBlocks([]*annotate.LineAnnotation{ann(1, "A"), ann(2, "A"), ann(9, "A")})
	if len(blocks) != 2 || blocks[1].Start != 9 {
		t.Fatalf("expected a break at the line-number gap, got %+v", blocks)
	}
}

// TestClassifyAuthor verifies agent-vs-human classification. Empty and the
// "auto" save-snapshot agent are human-origin; named agents are not.
func TestClassifyAuthor(t *testing.T) {
	cases := []struct {
		agent     string
		wantLabel string
		wantAgent bool
	}{
		{"", "you", false},
		{"auto", "you", false},
		{"AUTO", "you", false},
		{"claude", "claude", true},
		{"agent", "agent", true},
		{"cursor", "cursor", true},
	}
	for _, c := range cases {
		label, isAgent := annotate.ClassifyAuthor(c.agent)
		if label != c.wantLabel || isAgent != c.wantAgent {
			t.Errorf("ClassifyAuthor(%q) = (%q,%v), want (%q,%v)", c.agent, label, isAgent, c.wantLabel, c.wantAgent)
		}
	}
}
