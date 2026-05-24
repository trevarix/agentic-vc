package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/archive"
	"github.com/spf13/cobra"
)

var importFrom string

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import AVC history from an archive",
	Long: `Merge snapshots, branches, and objects from a bundle into the current project.

Objects are written using content-addressed paths — already-present blobs are
silently skipped. Database rows are inserted with INSERT OR IGNORE so existing
snapshots with the same ID are left unchanged.

Examples:
  avc import --from my-project.avc.tar.gz
  avc import --from archive.avc.tar.gz --json`,
	RunE: runImport,
}

func init() {
	importCmd.Flags().StringVar(&importFrom, "from", "", "Path to the .avc.tar.gz bundle to import (required)")
	_ = importCmd.MarkFlagRequired("from")
}

func runImport(cmd *cobra.Command, args []string) error {
	root, err := requireInitializedProject()
	if err != nil {
		return err
	}

	// Validate bundle exists before doing any work.
	if _, err := os.Stat(importFrom); err != nil {
		return fmt.Errorf("bundle not found: %s", importFrom)
	}

	result, err := archive.Import(root, importFrom)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"bundle":         result.BundlePath,
			"project_name":   result.ProjectName,
			"snapshot_count": result.SnapshotCount,
			"object_count":   result.ObjectCount,
			"skipped_rows":   result.SkippedRows,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Imported"), bold(importFrom))
	fmt.Printf("  Project:   %s\n", result.ProjectName)
	fmt.Printf("  Snapshots: %d imported\n", result.SnapshotCount)
	fmt.Printf("  Objects:   %d copied\n", result.ObjectCount)
	if result.SkippedRows > 0 {
		fmt.Printf("  Skipped:   %d rows (already present)\n", result.SkippedRows)
	}
	fmt.Printf("\nRun %s to see imported snapshots.\n", bold("avc list --all"))
	return nil
}
