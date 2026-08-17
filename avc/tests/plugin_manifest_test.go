// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for the Claude plugin manifest at .claude-plugin/plugin.json. The
// manifest ships to plugin users independently of the Go build, so nothing
// else catches a stale version or a component path that no longer resolves.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pluginManifest is the subset of plugin.json this suite asserts on.
type pluginManifest struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Skills   []string `json:"skills"`
	Commands string   `json:"commands"`
	Hooks    string   `json:"hooks"`
}

// repoRoot returns the repository root, two levels above avc/tests.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// readPluginManifest parses .claude-plugin/plugin.json from the repo root.
func readPluginManifest(t *testing.T) (pluginManifest, string) {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin manifest: %v", err)
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse plugin manifest: %v", err)
	}
	return m, root
}

// changelogVersionPattern matches a released CHANGELOG heading, skipping the
// unreleased section, which carries no version number.
var changelogVersionPattern = regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)

// TestPluginVersionMatchesChangelog guards the update path. Claude pins an
// installed plugin to the manifest version string and ships no update until it
// changes, so a release that bumps only CHANGELOG.md strands existing users.
func TestPluginVersionMatchesChangelog(t *testing.T) {
	manifest, root := readPluginManifest(t)

	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	match := changelogVersionPattern.FindSubmatch(data)
	if match == nil {
		t.Fatal("no released version heading found in CHANGELOG.md")
	}
	latest := string(match[1])

	if manifest.Version != latest {
		t.Errorf("plugin.json version is %q but the latest CHANGELOG version is %q; bump the manifest on every release",
			manifest.Version, latest)
	}
}

// TestPluginComponentPathsResolve guards the payload. A manifest path that no
// longer exists installs a plugin whose skills and commands are silently
// absent — the plugin still loads, so nothing else reports the loss.
func TestPluginComponentPathsResolve(t *testing.T) {
	manifest, root := readPluginManifest(t)

	if len(manifest.Skills) == 0 {
		t.Error("plugin.json declares no skills; the plugin would ship no payload to Claude Desktop chat, which loads skills and nothing else")
	}

	dirs := append([]string{}, manifest.Skills...)
	if manifest.Commands != "" {
		dirs = append(dirs, manifest.Commands)
	}
	for _, rel := range dirs {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("plugin.json references %s, which does not resolve: %v", rel, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("plugin.json references %s, which is not a directory", rel)
		}
	}
}

// TestPluginHooksInvokeAnExistingCommand guards the hook wiring. The hook file
// names an avc subcommand as a bare string, so a rename would break every
// install silently — the hook simply fails at edit time on the user's machine.
func TestPluginHooksInvokeAnExistingCommand(t *testing.T) {
	manifest, root := readPluginManifest(t)

	if manifest.Hooks == "" {
		t.Fatal("plugin.json declares no hooks file")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest.Hooks)))
	if err != nil {
		t.Fatalf("read hooks file %s: %v", manifest.Hooks, err)
	}

	var config struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse hooks file: %v", err)
	}

	events, ok := config.Hooks["PreToolUse"]
	if !ok || len(events) == 0 {
		t.Fatal("hooks file declares no PreToolUse entries")
	}

	found := false
	for _, event := range events {
		for _, h := range event.Hooks {
			if h.Command == "" {
				continue
			}
			found = true
			// The command must be one this binary actually serves.
			if !strings.HasPrefix(h.Command, "avc hook ") {
				t.Errorf("hook command %q does not invoke an avc hook subcommand", h.Command)
				continue
			}
			sub := strings.TrimPrefix(h.Command, "avc hook ")
			if sub != "pre-edit" {
				t.Errorf("hook command %q names unknown subcommand %q", h.Command, sub)
			}
		}
	}
	if !found {
		t.Error("PreToolUse entries declare no command")
	}
}

// TestPluginSkillsHaveSkillFiles guards each skill. Claude loads a skill
// directory only when it contains SKILL.md; one without it is skipped silently.
func TestPluginSkillsHaveSkillFiles(t *testing.T) {
	manifest, root := readPluginManifest(t)

	for _, rel := range manifest.Skills {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("read skills directory %s: %v", rel, err)
			continue
		}
		found := false
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			found = true
			skillFile := filepath.Join(dir, e.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				t.Errorf("skill %s/%s has no SKILL.md and would not load", rel, e.Name())
			}
		}
		if !found {
			t.Errorf("skills directory %s contains no skill directories", rel)
		}
	}
}

// TestPluginShipsEmbeddedSkills ties the two distribution paths together. The
// skills `avc init --skills` embeds and the skills the plugin publishes must be
// the same files, or guidance drifts between CLI users and plugin users.
func TestPluginShipsEmbeddedSkills(t *testing.T) {
	manifest, root := readPluginManifest(t)

	const embedded = "./avc/internal/skills/assets/skills/"
	present := false
	for _, rel := range manifest.Skills {
		if rel == embedded {
			present = true
			break
		}
	}
	if !present {
		t.Fatalf("plugin.json skills %v does not include %s; the plugin would ship a separate copy of the skills that `avc init --skills` installs",
			manifest.Skills, embedded)
	}

	// The embedded directory must hold every skill the CLI installs.
	dir := filepath.Join(root, filepath.FromSlash(embedded))
	for _, name := range []string{"avc-snapshot", "avc-restore", "avc-branch", "avc-merge", "avc-run"} {
		if _, err := os.Stat(filepath.Join(dir, name, "SKILL.md")); err != nil {
			t.Errorf("bundled skill %s is missing: %v", name, err)
		}
	}
}
