// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression tests: a large file (over the inline-preview cap) with a small
// real change must report exact +/- counts, not the whole file. Before the
// count/preview cap split, any file over 2000 lines reported every line as
// changed — inflating diffs and misattributed by agents to line endings.
package tests

import (
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/diff"
)

func TestDiff_LargeFile_OneLineChange_ExactCounts(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	var b strings.Builder
	for i := 0; i < 2500; i++ { // over maxPreviewLines (2000)
		b.WriteString("unchanged line of source code\n")
	}
	writeFile(t, projectRoot, "big.py", b.String())
	s1 := createMainSnap(t, projectRoot, mainBranchID, "big1")

	changed := strings.Replace(b.String(),
		"unchanged line of source code\n", "CHANGED THIS ONE LINE\n", 1)
	writeFile(t, projectRoot, "big.py", changed)
	s2 := createMainSnap(t, projectRoot, mainBranchID, "big2")

	res, err := diff.Compare(projectRoot, s1.ID, s2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(res.Files))
	}
	fd := res.Files[0]
	if fd.LinesAdded != 1 || fd.LinesRemoved != 1 {
		t.Errorf("large file, one-line change: +%d -%d, want +1 -1", fd.LinesAdded, fd.LinesRemoved)
	}
	if fd.CountsEstimated {
		t.Error("counts should be exact for a 2500-line file, not estimated")
	}
	// The inline preview is still capped by memory — omitted above 2000 lines.
	if fd.DiffPreview != "" {
		t.Error("preview should be omitted for a file over the preview cap")
	}
}

// A pure CRLF->LF change on a large file must count as no change: SplitLines
// normalizes line endings before counting, and exact counting now runs on
// large files too.
func TestDiff_LargeFile_CRLFOnly_NoChange(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	var crlf strings.Builder
	for i := 0; i < 2500; i++ {
		crlf.WriteString("unchanged line of source code\r\n")
	}
	writeFile(t, projectRoot, "big.js", crlf.String())
	s1 := createMainSnap(t, projectRoot, mainBranchID, "big1")

	writeFile(t, projectRoot, "big.js", strings.ReplaceAll(crlf.String(), "\r\n", "\n"))
	s2 := createMainSnap(t, projectRoot, mainBranchID, "big2")

	res, err := diff.Compare(projectRoot, s1.ID, s2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 changed file (hash differs), got %d", len(res.Files))
	}
	fd := res.Files[0]
	if fd.LinesAdded != 0 || fd.LinesRemoved != 0 {
		t.Errorf("pure CRLF->LF on large file: +%d -%d, want +0 -0", fd.LinesAdded, fd.LinesRemoved)
	}
}
