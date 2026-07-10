// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package policy evaluates the [protect] configuration: which paths
// agent-driven integration must not change. Like run.enabled, enforcement is
// mechanical — the checks here are called by merge (the hard gate) and by
// the snapshot/diff/status surfaces (early warnings), and nothing an agent
// can pass through MCP lifts them.
package policy

import (
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
)

// Modes for ProtectConfig.Mode.
const (
	ModeBlock = "block"
	ModeWarn  = "warn"
)

// Mode returns the effective enforcement mode: "block" unless the config
// explicitly says "warn". An unset or unknown value fails safe to "block" —
// a typo in config.toml must never silently disable protection.
func Mode(cfg *config.Config) string {
	if cfg != nil && cfg.Protect.Mode == ModeWarn {
		return ModeWarn
	}
	return ModeBlock
}

// Enabled reports whether any protected paths are configured.
func Enabled(cfg *config.Config) bool {
	return cfg != nil && len(cfg.Protect.Paths) > 0
}

// Check returns the subset of paths (slash-separated, project-relative) that
// match the configured protected globs, preserving input order. Returns nil
// when no protection is configured.
func Check(cfg *config.Config, paths []string) []string {
	if !Enabled(cfg) {
		return nil
	}
	rules := fileutil.CompilePatterns(cfg.Protect.Paths)
	var matched []string
	for _, p := range paths {
		if rules.Matches(p) || rules.MatchesDir(p) {
			matched = append(matched, p)
		}
	}
	return matched
}
