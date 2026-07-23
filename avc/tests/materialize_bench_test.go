// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package tests

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
)

// BenchmarkBranchCreate_NoBase measures workspace materialization on the
// copy path (branch created before any main snapshot): 2000 files across
// 20 directories, ~4 KB each.
func BenchmarkBranchCreate_NoBase(b *testing.B) {
	projectRoot := b.TempDir()
	if _, err := db.InitProject(projectRoot); err != nil {
		b.Fatal(err)
	}
	store, err := db.Open(projectRoot)
	if err != nil {
		b.Fatal(err)
	}
	proj, err := store.GetProject(projectRoot)
	if err != nil {
		store.Close()
		b.Fatal(err)
	}
	if _, err := store.EnsureMainBranch(proj.ID); err != nil {
		store.Close()
		b.Fatal(err)
	}
	store.Close()

	filler := bytes.Repeat([]byte("x"), 4096)
	for i := 0; i < 2000; i++ {
		rel := fmt.Sprintf("src/pkg%02d/file%04d.txt", i%20, i)
		content := append([]byte(fmt.Sprintf("file %d\n", i)), filler...)
		if err := fileutil.WriteFile(filepath.Join(projectRoot, rel), content); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := branch.Create(projectRoot, fmt.Sprintf("bench/no-base-%d", i), ""); err != nil {
			b.Fatal(err)
		}
	}
}
