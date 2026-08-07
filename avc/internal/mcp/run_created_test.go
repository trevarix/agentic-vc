// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewlyCreatedFiles(t *testing.T) {
	before := map[string]bool{"a.go": true, "b.go": true}
	after := map[string]bool{"a.go": true, "b.go": true, "media/x.jpg": true, "media/y.jpg": true}

	got := newlyCreatedFiles(before, after)
	want := []string{"media/x.jpg", "media/y.jpg"} // sorted
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestTrackedFileSet_RespectsIgnore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".avcignore"), "build/\n")
	mustWrite(t, filepath.Join(dir, "src", "app.go"), "package main")
	mustWrite(t, filepath.Join(dir, "build", "out.o"), "binary")

	set := trackedFileSet(dir, dir)

	if !set["src/app.go"] {
		t.Error("src/app.go should be tracked")
	}
	if set["build/out.o"] {
		t.Error("build/out.o is ignored and must not be in the tracked set")
	}
}

// TestTrackedFileSet_DetectsTestOutput models the reported pollution: a test
// run drops output files into the workspace; a before/after comparison
// surfaces exactly those.
func TestTrackedFileSet_DetectsTestOutput(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app.go"), "package main")
	before := trackedFileSet(dir, dir)

	// "test run" writes upload artifacts.
	for _, f := range []string{"media/1.jpg", "media/2.jpg", "media/3.jpg"} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(f)), "artifact")
	}
	created := newlyCreatedFiles(before, trackedFileSet(dir, dir))

	if len(created) != 3 {
		t.Fatalf("expected 3 created files, got %d: %v", len(created), created)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
