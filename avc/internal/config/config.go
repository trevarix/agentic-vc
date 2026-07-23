// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config manages the per-project AVC configuration file (.avc/config.toml).
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const configFile = ".avc/config.toml"

// Config holds all runtime configuration for an AVC project.
type Config struct {
	Project   ProjectConfig   `toml:"project"`
	Ignore    IgnoreConfig    `toml:"ignore"`
	Branch    BranchConfig    `toml:"branch"`
	Run       RunConfig       `toml:"run"`
	Retention RetentionConfig `toml:"retention"`
	Hooks     HooksConfig     `toml:"hooks"`
	Snapshot  SnapshotConfig  `toml:"snapshot"`
	Protect   ProtectConfig   `toml:"protect"`
	Watch     WatchConfig     `toml:"watch"`
}

// WatchConfig controls the `avc watch` continuous-checkpointing daemon.
type WatchConfig struct {
	// DebounceSeconds is the quiet period after the last file change before
	// a checkpoint snapshot is taken. 0 = DefaultWatchDebounceSeconds.
	DebounceSeconds int `toml:"debounce_seconds"`
	// MinIntervalSeconds is the minimum time between two watch snapshots on
	// the same branch, regardless of change volume. 0 = DefaultWatchMinIntervalSeconds.
	MinIntervalSeconds int `toml:"min_interval_seconds"`
	// IncludeWorkspaces also watches every active branch workspace, not just
	// the project root. Defaults to true (set include_workspaces = false to disable).
	IncludeWorkspaces *bool `toml:"include_workspaces"`
}

// Watch daemon defaults, applied when the corresponding config value is 0.
const (
	DefaultWatchDebounceSeconds    = 30
	DefaultWatchMinIntervalSeconds = 120
)

// ProtectConfig bounds what agent-driven integration may change. Paths
// matching these globs are refused (mode "block") or flagged (mode "warn")
// when a merge would modify them — enforced mechanically, like run.enabled:
// agents cannot lift it, only a human editing config.toml or passing the
// CLI-only --allow-protected flag can.
type ProtectConfig struct {
	// Paths are gitignore-style globs (same syntax as .avcignore, including
	// ** and trailing-/ for directories) naming files agents must not change.
	Paths []string `toml:"paths"`
	// Mode is "block" (default — merges touching protected paths are
	// refused) or "warn" (merges proceed with a prominent warning).
	Mode string `toml:"mode"`
}

// SnapshotConfig controls snapshot creation behavior.
type SnapshotConfig struct {
	// MaxFileSizeMB is the largest single file (in MB) a snapshot will read
	// and store. Larger files are skipped with a warning rather than risking
	// an out-of-memory read. 0 falls back to DefaultMaxFileSizeMB.
	MaxFileSizeMB int `toml:"max_file_size_mb"`
}

// DefaultMaxFileSizeMB is used when SnapshotConfig.MaxFileSizeMB is unset (0).
const DefaultMaxFileSizeMB = 100

// HooksConfig defines shell commands to run before/after snapshots and restores.
// Pre-hooks abort the operation on non-zero exit; post-hooks are non-fatal.
type HooksConfig struct {
	// PreSnapshot runs before a snapshot is created. Non-zero exit aborts the snapshot.
	PreSnapshot string `toml:"pre_snapshot"`
	// PostSnapshot runs after a successful snapshot. AVC_SNAPSHOT_ID is set in the environment.
	PostSnapshot string `toml:"post_snapshot"`
	// PreRestore runs before a restore. Non-zero exit aborts the restore.
	PreRestore string `toml:"pre_restore"`
	// PostRestore runs after a successful restore. AVC_SNAPSHOT_ID is set in the environment.
	PostRestore string `toml:"post_restore"`
}

// RetentionConfig controls automatic snapshot pruning per branch.
type RetentionConfig struct {
	// MaxSnapshotsPerBranch is the maximum number of snapshots to keep per
	// branch. When exceeded, the oldest snapshots are deleted. 0 = unlimited.
	MaxSnapshotsPerBranch int `toml:"max_snapshots_per_branch"`

	// MaxAgeDays deletes snapshots older than N days. 0 = unlimited.
	MaxAgeDays int `toml:"max_age_days"`

	// AutoGC runs garbage collection automatically after pruning.
	// Defaults to true when a pruning policy is active.
	AutoGC bool `toml:"auto_gc"`

	// MaxWatchSnapshotsPerBranch caps snapshots created by `avc watch`
	// (label prefix "auto:watch") per branch — the oldest watch snapshots
	// are pruned first, before any other retention rule considers them.
	// 0 = the built-in default (200); -1 = unlimited.
	MaxWatchSnapshotsPerBranch int `toml:"max_watch_snapshots_per_branch"`
}

// DefaultMaxWatchSnapshotsPerBranch is used when MaxWatchSnapshotsPerBranch
// is unset (0). Watch snapshots are high-volume by design, so unlike the
// other retention rules this cap is on by default.
const DefaultMaxWatchSnapshotsPerBranch = 200

// RunConfig holds workspace command runner settings.
type RunConfig struct {
	// Enabled gates the avc_run_in_workspace MCP tool. Default false.
	// Must be set to true by a human in .avc/config.toml — agents cannot
	// enable it themselves.
	Enabled               bool `toml:"enabled"`
	DefaultTimeoutSeconds int  `toml:"default_timeout_seconds"`
	MaxTimeoutSeconds     int  `toml:"max_timeout_seconds"`
	MaxOutputKB           int  `toml:"max_output_kb"`
}

// ProjectConfig holds project-level settings.
type ProjectConfig struct {
	DefaultAgent string `toml:"default_agent"`
}

// IgnoreConfig lists additional patterns to ignore beyond .avcignore.
type IgnoreConfig struct {
	ExtraPatterns []string `toml:"extra_patterns"`
}

// BranchConfig holds branch settings.
type BranchConfig struct {
	Active string `toml:"active"`
}

// defaultConfig is written during `avc init`.
var defaultConfig = Config{
	Project: ProjectConfig{DefaultAgent: ""},
	Ignore:  IgnoreConfig{ExtraPatterns: []string{}},
	Branch:  BranchConfig{Active: "main"},
}

// Load reads and parses the config file from the project root.
// Missing config files are silently ignored; defaults are returned.
func Load(projectRoot string) (*Config, error) {
	path := filepath.Join(projectRoot, configFile)
	cfg := defaultConfig

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}
	// Ensure Active is set if the file predates Phase 4.
	if cfg.Branch.Active == "" {
		cfg.Branch.Active = "main"
	}
	return &cfg, nil
}

// Save writes cfg to the config file, overwriting any existing content.
func Save(projectRoot string, cfg *Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(projectRoot, configFile), buf.Bytes(), 0644)
}

// SetActiveBranch atomically updates the active branch name in the config file.
// A spin-lock file serialises concurrent writers so that simultaneous
// branch-switch calls (e.g. from two agents) do not corrupt config.toml.
func SetActiveBranch(projectRoot, name string) error {
	lockPath := filepath.Join(projectRoot, ".avc", "config.lock")
	unlock, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("acquire config lock: %w", err)
	}
	defer unlock()

	cfg, err := Load(projectRoot)
	if err != nil {
		c := defaultConfig
		cfg = &c
	}
	cfg.Branch.Active = name
	return Save(projectRoot, cfg)
}

// acquireLock creates a lock file at lockPath, spinning with 10 ms retries
// for up to 500 ms. Returns a release function that removes the lock file.
//
// Stale lock files (older than 30 s — left by a crashed process) are silently
// removed so callers are never permanently blocked by a dead writer.
func acquireLock(lockPath string) (func(), error) {
	const (
		maxWait    = 500 * time.Millisecond
		retryDelay = 10 * time.Millisecond
		staleAfter = 30 * time.Second
	)

	deadline := time.Now().Add(maxWait)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// Lock acquired.
			f.Close()
			return func() { os.Remove(lockPath) }, nil //nolint:errcheck
		}

		if !os.IsExist(err) {
			// Unexpected error (permissions, full disk, etc.).
			return nil, err
		}

		// Lock file exists — check whether it is stale (from a crashed process).
		if info, statErr := os.Stat(lockPath); statErr == nil {
			if time.Since(info.ModTime()) > staleAfter {
				os.Remove(lockPath) //nolint:errcheck  — best effort, retry on next loop
				continue
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"timeout waiting for config lock after %s (stale lock at %s?)",
				maxWait, lockPath,
			)
		}
		time.Sleep(retryDelay)
	}
}

// WriteDefault writes the default config.toml to the project's .avc/ directory,
// a default .avcignore to the project root, and adds .avc/ and .avcignore to
// .gitignore — appending to an existing file, or creating one when the project
// is inside a git repository.
func WriteDefault(projectRoot string) error {
	avcDir := filepath.Join(projectRoot, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		return err
	}

	if err := writeFileIfAbsent(filepath.Join(projectRoot, configFile), defaultTOML); err != nil {
		return err
	}

	if err := writeFileIfAbsent(filepath.Join(projectRoot, ".avcignore"), defaultAVCIgnore); err != nil {
		return err
	}

	_, err := AppendToGitignore(projectRoot, []string{".avc/", ".avcignore"})
	return err
}

// writeFileIfAbsent writes content to path only if the file does not yet exist.
func writeFileIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// gitignoreHeader labels the block of AVC-written .gitignore entries.
const gitignoreHeader = "# Agentic Version Control"

// UserOwnsGitignore reports whether the project has a .gitignore that AVC did
// not create. An AVC-created .gitignore always starts with the AVC header;
// one that starts with anything else was authored by the user, which means
// the user has an expressed tracking policy that must be respected.
func UserOwnsGitignore(projectRoot string) bool {
	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		return false
	}
	return !bytes.HasPrefix(bytes.TrimSpace(data), []byte(gitignoreHeader))
}

// AppendToGitignore adds the given entries to the project's .gitignore under
// an "# Agentic Version Control" comment, skipping entries already present.
// Appends to an existing .gitignore; when none exists, one is created — but
// only when the project is inside a git repository, since without git the
// file would be noise. Returns "created", "updated", or "" when nothing was
// written.
func AppendToGitignore(projectRoot string, entries []string) (string, error) {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if os.IsNotExist(err) {
		if !insideGitRepo(projectRoot) {
			return "", nil
		}
		data = nil
	} else if err != nil {
		return "", err
	}

	existing := make(map[string]bool)
	for _, line := range splitLines(string(data)) {
		existing[line] = true
	}

	var toAppend []string
	for _, entry := range entries {
		if !existing[entry] {
			toAppend = append(toAppend, entry)
		}
	}
	if len(toAppend) == 0 {
		return "", nil
	}

	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Repeat the comment header only if a previous append didn't write it.
	block := ""
	if !existing[gitignoreHeader] {
		block = gitignoreHeader + "\n"
	}
	if len(data) > 0 {
		block = "\n" + block
	}
	for _, entry := range toAppend {
		block += entry + "\n"
	}
	if _, err := f.WriteString(block); err != nil {
		return "", err
	}
	if len(data) > 0 {
		return "updated", nil
	}
	return "created", nil
}

// insideGitRepo reports whether dir is inside a git repository — a .git
// directory (or file, for worktrees and submodules) exists in dir or any
// parent.
func insideGitRepo(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

const defaultTOML = `# AVC configuration file

[project]
# default_agent = "my-agent"

[ignore]
# extra_patterns = ["*.log", "dist/"]

[branch]
active = "main"

[run]
# Allow avc_run_in_workspace to execute commands in agent workspaces.
# Must be set to true by a human — agents cannot enable this themselves.
# enabled = false

# Maximum time a workspace command can run before being killed.
default_timeout_seconds = 180
max_timeout_seconds     = 600

# Maximum output captured per stream (stdout/stderr) before truncation.
# Increase for projects with verbose test suites.
max_output_kb = 512

[snapshot]
# Files larger than this are skipped (with a warning) instead of being
# read and stored — protects against an out-of-memory read on an
# accidentally-tracked large binary. 0 = use the built-in default (100 MB).
# max_file_size_mb = 100

[protect]
# Paths agents must not change. Merges that would modify a matching path are
# refused (mode = "block", the default) or flagged (mode = "warn"). Globs use
# .avcignore syntax, including ** and trailing / for directories.
# A human can override a blocked merge with: avc merge <branch> --allow-protected
# (the MCP merge tool has no such override — agents cannot lift this).
# paths = [".github/workflows/**", "secrets/**", "*.pem"]
# mode  = "block"

[watch]
# Settings for the continuous-checkpointing daemon (avc watch).
# Quiet period after the last file change before a checkpoint is taken.
# debounce_seconds = 30

# Minimum time between two watch snapshots on the same branch.
# min_interval_seconds = 120

# Also watch every active branch workspace, not just the project root.
# include_workspaces = true

[retention]
# Maximum snapshots to keep per branch (oldest pruned first). 0 = unlimited.
# max_snapshots_per_branch = 100

# Delete snapshots older than N days. 0 = unlimited.
# max_age_days = 90

# Run gc automatically after pruning. Default: true.
# auto_gc = true

# Cap on snapshots created by "avc watch" per branch — these are pruned
# first. 0 = the built-in default (200); -1 = unlimited.
# max_watch_snapshots_per_branch = 200

[hooks]
# Shell commands run around snapshots and restores.
# Pre-hooks: non-zero exit aborts the operation.
# Post-hooks: non-zero exit is logged to stderr but does not fail the operation.
# Environment: AVC_PROJECT_ROOT, AVC_SNAPSHOT_ID, AVC_BRANCH are always set.

# pre_snapshot  = "npm test -- --silent"
# post_snapshot = ""
# pre_restore   = ""
# post_restore  = ""
`

const defaultAVCIgnore = `# AVC ignore rules — patterns listed here are excluded from all snapshots.
# Syntax is identical to .gitignore.

# ── Python ────────────────────────────────────────────────────────────────────
.venv/
venv/
env/
.env/
__pycache__/
*.pyc
*.pyo
*.pyd
*.egg-info/
.pytest_cache/
.mypy_cache/
.ruff_cache/
.tox/
htmlcov/
.coverage

# ── Node / JavaScript / TypeScript ────────────────────────────────────────────
node_modules/
.npm/
.next/
.nuxt/
.svelte-kit/
*.tsbuildinfo

# ── Go ────────────────────────────────────────────────────────────────────────
vendor/

# ── Rust ──────────────────────────────────────────────────────────────────────
target/

# ── Java / Kotlin / Gradle / Maven ────────────────────────────────────────────
.gradle/
.mvn/
*.class
*.jar

# ── .NET / C# ─────────────────────────────────────────────────────────────────
obj/

# ── Ruby ──────────────────────────────────────────────────────────────────────
.bundle/

# ── Terraform ─────────────────────────────────────────────────────────────────
.terraform/
*.tfstate
*.tfstate.backup

# ── Build output (cross-stack) ────────────────────────────────────────────────
dist/
build/
out/
bin/
*.exe
*.dll
*.so
*.dylib
*.o
*.a

# ── Logs and coverage ─────────────────────────────────────────────────────────
*.log
logs/
coverage/

# ── Environment and secrets ───────────────────────────────────────────────────
.env
.env.*
.env.local
.env.production
*.pem
*.key
*.cert
*.p12
*.pfx

# ── OS artifacts ──────────────────────────────────────────────────────────────
.DS_Store
Thumbs.db
desktop.ini

# ── Editor metadata ───────────────────────────────────────────────────────────
.idea/
*.swp
*.swo
*.suo
*.user
`
