// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/fileutil"
	"github.com/spf13/cobra"
)

var checkIgnoreCmd = &cobra.Command{
	Use:   "check-ignore <path>...",
	Short: "Report whether paths are excluded from snapshots, and by which rule",
	Long: `Reports, for each given path, whether it is excluded from AVC snapshots by
an .avcignore rule — and if so, which pattern is responsible.

This is AVC's analog of 'git check-ignore'. Use it to diagnose why an
expected file is missing from a snapshot, branch workspace, or diff: an
over-broad ignore pattern (e.g. an unanchored 'vendor/') silently excludes
source, and this command names the exact rule.

Paths are interpreted relative to the active branch's source directory (the
workspace on a branch, the project root on main). Ignore rules are the root
.avcignore layered with the workspace's, exactly as a snapshot sees them.

Exit code is 0 when at least one given path is ignored, 1 when none are
(mirroring git check-ignore), so it is usable in scripts.

  avc check-ignore web/features/vendor/screen.tsx
  avc check-ignore --json src/main.go vendor/pkg/x.go`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCheckIgnore,
}

func init() {
	rootCmd.AddCommand(checkIgnoreCmd)
}

// checkIgnoreResult is one path's outcome.
type checkIgnoreResult struct {
	Path    string `json:"path"`
	Ignored bool   `json:"ignored"`
	Pattern string `json:"pattern,omitempty"`
}

func runCheckIgnore(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branchName := branchpkg.GetActiveBranchName(projectPath)
	sourceDir := branchpkg.WorkspacePath(projectPath, branchName) // "" for main
	if sourceDir == "" {
		sourceDir = projectPath
	}

	rules, err := fileutil.LoadLayeredIgnoreRules(projectPath, sourceDir)
	if err != nil {
		return fmt.Errorf("load ignore rules: %w", err)
	}

	results := make([]checkIgnoreResult, 0, len(args))
	anyIgnored := false
	for _, arg := range args {
		rel := normalizeCheckPath(sourceDir, arg)
		pattern, ignored := rules.WhyIgnored(rel)
		if ignored {
			anyIgnored = true
		}
		results = append(results, checkIgnoreResult{Path: rel, Ignored: ignored, Pattern: pattern})
	}

	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"results": results,
			"success": true,
		}); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			if r.Ignored {
				fmt.Printf("%s  %s %s\n", warn("ignored"), cyan(r.Path),
					dim("(matched by '"+r.Pattern+"')"))
			} else {
				fmt.Printf("%s  %s\n", green("tracked"), cyan(r.Path))
			}
		}
	}

	// git check-ignore semantics: success (0) when at least one path is
	// ignored, 1 when none are. Signal "none ignored" without printing a
	// spurious error.
	if !anyIgnored {
		os.Exit(1)
	}
	return nil
}

// normalizeCheckPath converts a user-supplied path to a slash-separated path
// relative to sourceDir. Absolute paths and paths inside sourceDir are made
// relative; anything else is returned cleaned as-is (already relative).
func normalizeCheckPath(sourceDir, arg string) string {
	if abs, err := filepath.Abs(arg); err == nil {
		if rel, err := filepath.Rel(sourceDir, abs); err == nil && !filepath.IsAbs(rel) &&
			!startsWithParent(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(arg))
}

// startsWithParent reports whether a relative path escapes its base (begins
// with "..").
func startsWithParent(rel string) bool {
	return rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}
