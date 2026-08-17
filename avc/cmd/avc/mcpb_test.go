// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Tests for the Claude Desktop extension manifest at mcpb/manifest.json.
//
// The manifest hard-codes the command line Desktop uses to launch the server.
// Nothing else links the two: rename a flag or stop accepting positional
// search roots and the manifest keeps shipping the old argv, failing only on
// a user's machine after they install the bundle. This test runs the
// manifest's own argv through the real command tree so that breaks the build
// instead.
//
// It lives in package avc because rootCmd is unexported.
package avc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mcpbManifest is the subset of the manifest this test asserts on.
type mcpbManifest struct {
	ManifestVersion string `json:"manifest_version"`
	Name            string `json:"name"`
	Server          struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcp_config"`
	} `json:"server"`
	UserConfig map[string]struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Multiple bool   `json:"multiple"`
		Required bool   `json:"required"`
	} `json:"user_config"`
}

// userConfigPlaceholder is how the manifest refers to a value the host
// substitutes at launch. It is not an argument the CLI ever sees.
const userConfigPlaceholder = "${user_config."

func readMCPBManifest(t *testing.T) mcpbManifest {
	t.Helper()
	path := filepath.Join("..", "..", "..", "mcpb", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m mcpbManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

// TestMCPBManifestArgsParse is the guard that matters. It resolves the
// manifest's argv against the real command tree and parses its flags, so a
// renamed flag or a removed subcommand fails here rather than on a user's
// machine.
func TestMCPBManifestArgsParse(t *testing.T) {
	m := readMCPBManifest(t)

	args := m.Server.MCPConfig.Args
	if len(args) == 0 {
		t.Fatal("manifest declares no args")
	}

	// Host placeholders expand to user-chosen directories at launch. Drop them
	// and keep a stand-in so the positional-argument check stays meaningful.
	var cli []string
	placeholders := 0
	for _, a := range args {
		if strings.Contains(a, userConfigPlaceholder) {
			placeholders++
			continue
		}
		cli = append(cli, a)
	}
	if placeholders == 0 {
		t.Error("manifest passes no user_config value; the server would have no search roots")
	}

	cmd, remaining, err := rootCmd.Find(cli)
	if err != nil {
		t.Fatalf("manifest argv %v does not resolve to a command: %v", cli, err)
	}
	if cmd == rootCmd {
		t.Fatalf("manifest argv %v resolved to the root command, not a subcommand", cli)
	}
	if got := cmd.CommandPath(); got != "avc mcp serve" {
		t.Errorf("manifest argv resolves to %q, want %q", got, "avc mcp serve")
	}

	if err := cmd.ParseFlags(remaining); err != nil {
		t.Fatalf("manifest flags %v rejected by %s: %v", remaining, cmd.CommandPath(), err)
	}

	// The search roots arrive as positional arguments, one per configured
	// folder. The command must accept them.
	positional := append(cmd.Flags().Args(), "/tmp/projects", "/tmp/work")
	if cmd.Args != nil {
		if err := cmd.Args(cmd, positional); err != nil {
			t.Errorf("%s rejects search roots as positional arguments: %v", cmd.CommandPath(), err)
		}
	}
}

// TestMCPBManifestShape checks the fields Claude Desktop requires to install
// and launch the bundle at all.
func TestMCPBManifestShape(t *testing.T) {
	m := readMCPBManifest(t)

	if m.ManifestVersion == "" {
		t.Error("manifest_version is empty")
	}
	if m.Name == "" {
		t.Error("name is empty")
	}
	if m.Server.Type != "binary" {
		t.Errorf("server.type is %q, want %q", m.Server.Type, "binary")
	}

	// entry_point and command must agree, or Desktop validates one path and
	// executes another.
	if m.Server.EntryPoint != m.Server.MCPConfig.Command {
		t.Errorf("entry_point %q and mcp_config.command %q disagree",
			m.Server.EntryPoint, m.Server.MCPConfig.Command)
	}
	// The build script places the binary at this path inside the archive.
	if want := "server/avc"; m.Server.EntryPoint != want {
		t.Errorf("entry_point is %q, want %q — the build script writes the binary there",
			m.Server.EntryPoint, want)
	}
}

// TestMCPBUserConfigCollectsFolders covers the install-time prompt. This is
// the only moment a Desktop user is told a project folder is needed, so the
// field must be a required, multi-value directory picker.
func TestMCPBUserConfigCollectsFolders(t *testing.T) {
	m := readMCPBManifest(t)

	// Every placeholder in args must name a config key that exists, or the
	// host substitutes nothing and the server starts with no roots.
	for _, a := range m.Server.MCPConfig.Args {
		if !strings.Contains(a, userConfigPlaceholder) {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(a, userConfigPlaceholder), "}")
		field, ok := m.UserConfig[key]
		if !ok {
			t.Errorf("args reference user_config.%s, which is not defined", key)
			continue
		}
		if field.Type != "directory" {
			t.Errorf("user_config.%s is type %q, want %q so the user gets a folder picker",
				key, field.Type, "directory")
		}
		if !field.Multiple {
			t.Errorf("user_config.%s is not multiple; one install could then cover only one project folder", key)
		}
		if !field.Required {
			t.Errorf("user_config.%s is not required; the user would never be asked for it", key)
		}
		if field.Title == "" {
			t.Errorf("user_config.%s has no title; the install prompt would be unlabelled", key)
		}
	}
}
