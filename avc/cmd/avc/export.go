package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/archive"
	"github.com/spf13/cobra"
)

var (
	exportOutput string
	exportBranch string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export AVC history to a portable archive",
	Long: `Export snapshots, branches, and file objects to a .tar.gz bundle.

The bundle can be imported on another machine with: avc import --from <file>

Examples:
  avc export                                   # full export (auto-named)
  avc export --output my-project.avc.tar.gz    # full export, explicit name
  avc export --branch feature/auth             # export one branch only`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Output file path (default: <project>-<timestamp>.avc.tar.gz)")
	exportCmd.Flags().StringVar(&exportBranch, "branch", "", "Export only this branch's snapshots (default: all branches)")
}

func runExport(cmd *cobra.Command, args []string) error {
	root, err := requireInitializedProject()
	if err != nil {
		return err
	}

	out := exportOutput
	if out == "" {
		base := filepath.Base(root)
		out = fmt.Sprintf("%s-%d.avc.tar.gz", base, time.Now().Unix())
	}

	opts := archive.ExportOptions{
		BranchName: exportBranch,
		OutputPath: out,
	}

	manifest, err := archive.Export(root, opts)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"output":         out,
			"project_name":   manifest.ProjectName,
			"branches":       manifest.Branches,
			"snapshot_count": manifest.SnapshotCount,
			"object_count":   manifest.ObjectCount,
		})
	}

	info, _ := os.Stat(out)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	fmt.Printf("Exported %s\n", bold(out))
	fmt.Printf("  Project:   %s\n", manifest.ProjectName)
	if exportBranch != "" {
		fmt.Printf("  Branch:    %s\n", exportBranch)
	} else {
		fmt.Printf("  Branches:  %d (%s)\n", len(manifest.Branches), joinTrunc(manifest.Branches, 3))
	}
	fmt.Printf("  Snapshots: %d\n", manifest.SnapshotCount)
	fmt.Printf("  Objects:   %d\n", manifest.ObjectCount)
	fmt.Printf("  Size:      %s\n", formatBytes(size))
	fmt.Printf("\nImport on another machine with: avc import --from %s\n", out)
	return nil
}

// joinTrunc joins up to n strings and appends "…+N more" if the list is longer.
func joinTrunc(ss []string, n int) string {
	if len(ss) <= n {
		return strings.Join(ss, ", ")
	}
	return strings.Join(ss[:n], ", ") + fmt.Sprintf(" …+%d more", len(ss)-n)
}
