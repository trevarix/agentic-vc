// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "avc",
	Short: "Agentic Version Control — lightweight snapshots for projects",
	Long: `AVC lets you save, view, and restore project snapshots.
Designed for agents and non-technical users alike.

Use --json on any command for machine-readable output.`,
}

// Execute is the entry point called from main.go.
func Execute() error {
	return rootCmd.Execute()
}

// buildVersion holds version info injected by main.go at startup.
var buildVersion = struct {
	Version string
	Commit  string
	Date    string
}{"dev", "none", "unknown"}

// SetVersion is called by main.go to propagate ldflags values into the CLI.
func SetVersion(version, commit, date string) {
	buildVersion.Version = version
	buildVersion.Commit = commit
	buildVersion.Date = date
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	initHelp()

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(branchCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(mergeCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(storageCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(trashCmd)
}
