package avc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/gc"
	"github.com/spf13/cobra"
)

var gcRunFlag bool

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage-collect unreferenced objects from the object store",
	Long: `Scans .avc/objects/ and identifies blobs no longer referenced by any snapshot.

By default this is a dry run — it reports what would be deleted without removing anything.
Pass --run to actually delete the orphaned blobs and reclaim disk space.

Typical workflow after deleting branches or pruning snapshots:

  avc gc          # preview what would be removed
  avc gc --run    # delete and report bytes reclaimed`,
	RunE: runGC,
}

func init() {
	gcCmd.Flags().BoolVar(&gcRunFlag, "run", false, "Delete unreferenced objects (default is dry-run)")
}

func runGC(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	dryRun := !gcRunFlag
	result, err := gc.Run(projectPath, dryRun)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"scanned_objects": result.ScannedObjects,
			"deleted_objects": result.DeletedObjects,
			"bytes_reclaimed": result.BytesReclaimed,
			"dry_run":         result.DryRun,
		})
	}

	if result.DryRun {
		fmt.Fprintf(os.Stderr, "%s\n", dim("[avc] Dry run — pass --run to actually delete objects"))
	}

	if result.ScannedObjects == 0 {
		fmt.Println("Object store is empty — nothing to collect.")
		return nil
	}

	if result.DeletedObjects == 0 {
		fmt.Printf("%s All %d object(s) are referenced by active snapshots.\n",
			success("✓"), result.ScannedObjects)
		return nil
	}

	verb := "Would remove"
	if !result.DryRun {
		verb = "Removed"
	}
	fmt.Printf("%s %s %d unreferenced object(s) (%s)\n",
		success("✓"), verb,
		result.DeletedObjects,
		formatBytes(result.BytesReclaimed),
	)
	fmt.Printf("  Scanned: %d  ·  Kept: %d  ·  %s: %d\n",
		result.ScannedObjects,
		result.ScannedObjects-result.DeletedObjects,
		verb, result.DeletedObjects,
	)
	if result.DryRun {
		fmt.Printf("\n%s\n", dim("Run `avc gc --run` to delete these objects."))
	}
	return nil
}

// formatBytes converts a byte count to a human-readable string (e.g. "4.2 MB").
func formatBytes(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/GB)
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/MB)
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
