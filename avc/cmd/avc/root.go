package avc

import (
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

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(branchCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(mergeCmd)
	rootCmd.AddCommand(runCmd)
}
