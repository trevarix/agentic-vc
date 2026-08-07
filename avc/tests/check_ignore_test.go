// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for the ignore diagnostics behind `avc check-ignore`: WhyIgnored names
// the rule that excludes a path, including exclusion via an ignored ancestor
// directory, and LoadLayeredIgnoreRules layers root + workspace.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
)

func TestWhyIgnored_NamesTheMatchingPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".avcignore", "*.log\n/vendor/\nnode_modules/\n")
	rules, err := fileutil.LoadLayeredIgnoreRules(dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cases := []struct {
		rel         string
		wantIgnored bool
		wantPattern string
	}{
		{"app.log", true, "*.log"},                       // direct file match
		{"vendor/pkg.go", true, "/vendor/"},              // root-anchored dir
		{"web/features/vendor/screen.tsx", false, ""},    // nested vendor stays tracked
		{"web/node_modules/react/index.js", true, "node_modules/"}, // ignored ancestor at depth
		{"src/main.go", false, ""},                       // ordinary source
	}
	for _, c := range cases {
		pat, ignored := rules.WhyIgnored(c.rel)
		if ignored != c.wantIgnored {
			t.Errorf("%s: ignored=%v, want %v", c.rel, ignored, c.wantIgnored)
		}
		if ignored && pat != c.wantPattern {
			t.Errorf("%s: pattern=%q, want %q", c.rel, pat, c.wantPattern)
		}
	}
}

func TestLoadLayeredIgnoreRules_RootUnderWorkspace(t *testing.T) {
	projectRoot := t.TempDir()
	writeFile(t, projectRoot, ".avcignore", "build/\n")

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".avcignore"), []byte("media/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rules, err := fileutil.LoadLayeredIgnoreRules(projectRoot, workspace)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if pat, ok := rules.WhyIgnored("build/out.o"); !ok || pat != "build/" {
		t.Errorf("root rule build/ not applied: %q %v", pat, ok)
	}
	if pat, ok := rules.WhyIgnored("media/pic.bin"); !ok || pat != "media/" {
		t.Errorf("workspace rule media/ not applied: %q %v", pat, ok)
	}
	if _, ok := rules.WhyIgnored("src/app.go"); ok {
		t.Error("ordinary source should not be ignored")
	}
}
