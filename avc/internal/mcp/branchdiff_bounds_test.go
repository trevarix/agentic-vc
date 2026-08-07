// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"strings"
	"testing"

	diffpkg "github.com/trevarix/agentic-vc/avc/internal/diff"
)

// bigPreviewResult builds a diff Result whose full rendering (with previews)
// far exceeds the byte budget.
func bigPreviewResult(files int) *diffpkg.Result {
	preview := strings.Repeat("+a line of changed content that is reasonably long\n", 400) // ~20 KB each
	r := &diffpkg.Result{FromSnapshotID: "snap-from", ToSnapshotID: "snap-to"}
	for i := 0; i < files; i++ {
		r.Files = append(r.Files, &diffpkg.FileDiff{
			Path:        fmt.Sprintf("src/file%03d.go", i),
			Type:        diffpkg.Modified,
			LinesAdded:  400,
			LinesRemoved: 1,
			DiffPreview: preview,
		})
	}
	return r
}

func TestRenderBranchDiffBounded_FallsBackToStat(t *testing.T) {
	// 50 files x ~20 KB preview each ≈ 1 MB full — well over the budget.
	result := bigPreviewResult(50)

	full := formatBranchDiff("feat/x", "snap-from", "snap-to", result, false)
	if len(full) <= maxBranchDiffBytes {
		t.Fatalf("test setup: full diff should exceed the budget, got %d bytes", len(full))
	}

	out := renderBranchDiffBounded("feat/x", "snap-from", "snap-to", result, false)
	if len(out) > maxBranchDiffBytes {
		t.Errorf("bounded output %d bytes exceeds budget %d", len(out), maxBranchDiffBytes)
	}
	if !strings.Contains(out, "per-file summary") {
		t.Error("expected a note explaining the fallback to a summary")
	}
	// A stat summary keeps one line per file but drops the previews.
	if strings.Contains(out, "a line of changed content") {
		t.Error("stat fallback must not contain diff previews")
	}
	for _, want := range []string{"src/file000.go", "src/file049.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("stat summary should list %s", want)
		}
	}
}

func TestRenderBranchDiffBounded_TruncatesHugeFileList(t *testing.T) {
	// Enough files that even the stat summary (one line each) exceeds the budget.
	result := bigPreviewResult(20000)

	out := renderBranchDiffBounded("feat/x", "snap-from", "snap-to", result, false)
	if len(out) > maxBranchDiffBytes {
		t.Errorf("bounded output %d bytes exceeds budget %d", len(out), maxBranchDiffBytes)
	}
	if !strings.Contains(out, "more file(s) not shown") {
		t.Error("expected a truncation trailer for a huge file list")
	}
}

func TestRenderBranchDiffBounded_SmallDiffUnchanged(t *testing.T) {
	result := &diffpkg.Result{
		FromSnapshotID: "snap-from", ToSnapshotID: "snap-to",
		Files: []*diffpkg.FileDiff{
			{Path: "a.go", Type: diffpkg.Modified, LinesAdded: 1, LinesRemoved: 1, DiffPreview: "+x\n-y\n"},
		},
	}
	out := renderBranchDiffBounded("feat/x", "snap-from", "snap-to", result, false)
	if !strings.Contains(out, "+x") {
		t.Error("a small diff should be returned in full, with its preview")
	}
	if strings.Contains(out, "per-file summary") {
		t.Error("a small diff should not trigger the fallback note")
	}
}
