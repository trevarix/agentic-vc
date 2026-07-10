// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/fsck"
	"github.com/spf13/cobra"
)

var fsckRepair bool

var fsckCmd = &cobra.Command{
	Use:   "fsck",
	Short: "Verify object-store integrity (re-hash every stored blob)",
	Long: `Re-hashes every object in .avc/objects/ and reports any whose content no
longer matches its content-addressed filename (disk corruption, tampering,
or a torn write predating atomic object writes).

Pass --repair to quarantine corrupt objects to .avc/corrupt/ so nothing
dedupes against or restores from them; the affected snapshots are listed so
you know which history is damaged.

Exits non-zero when corruption is found, so fsck can gate CI or backups.`,
	RunE: runFsck,
}

func init() {
	fsckCmd.Flags().BoolVar(&fsckRepair, "repair", false, "Quarantine corrupt objects to .avc/corrupt/")
	rootCmd.AddCommand(fsckCmd)
}

func runFsck(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	result, err := fsck.Run(projectPath, fsckRepair)
	if err != nil {
		return fmt.Errorf("fsck: %w", err)
	}

	if jsonOutput {
		if result.Corrupt == nil {
			result.Corrupt = []fsck.CorruptObject{}
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return err
		}
	} else {
		if len(result.Corrupt) == 0 {
			fmt.Printf("%s All %d object(s) verified intact.\n", success("✓"), result.ScannedObjects)
		} else {
			fmt.Printf("%s %d of %d object(s) are CORRUPT:\n",
				failure("✗"), len(result.Corrupt), result.ScannedObjects)
			for _, c := range result.Corrupt {
				fmt.Printf("  %s %s\n", failure("!"), c.Hash)
				if c.QuarantinedTo != "" {
					fmt.Printf("      %s %s\n", dim("quarantined to"), dim(c.QuarantinedTo))
				}
				for _, snapID := range c.AffectedSnapshots {
					fmt.Printf("      %s %s\n", dim("damages snapshot"), cyan(snapID))
				}
			}
			if !fsckRepair {
				fmt.Printf("\n%s\n", dim("Run `avc fsck --repair` to quarantine corrupt objects to .avc/corrupt/."))
			}
		}
	}

	if len(result.Corrupt) > 0 {
		// Non-zero exit so CI/backup pipelines can gate on integrity.
		// SilenceErrors/Usage keep cobra from printing a spurious help screen.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return fmt.Errorf("%d corrupt object(s) found", len(result.Corrupt))
	}
	return nil
}
