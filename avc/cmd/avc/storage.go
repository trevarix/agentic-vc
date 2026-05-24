package avc

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var (
	storageByBranch   bool
	storageBySnapshot bool
	storageLimit      int
)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Show AVC disk usage for this project",
	Long: `Breaks down how much disk space AVC is using:

  - Database:   .avc/avc.db
  - Objects:    .avc/objects/ (content-addressed blobs)
  - Workspaces: .avc/workspaces/ (one directory per agent branch)

Use --by-branch to see a per-branch snapshot size summary from the database.
Use --by-snapshot --limit N to list the N largest individual snapshots.`,
	RunE: runStorage,
}

func init() {
	storageCmd.Flags().BoolVar(&storageByBranch, "by-branch", false, "Show per-branch breakdown")
	storageCmd.Flags().BoolVar(&storageBySnapshot, "by-snapshot", false, "Show per-snapshot breakdown")
	storageCmd.Flags().IntVar(&storageLimit, "limit", 10, "Maximum rows to show (with --by-snapshot)")
}

// storageSummary holds computed sizes for JSON output.
type storageSummary struct {
	ProjectName    string                `json:"project_name"`
	DatabaseBytes  int64                 `json:"database_bytes"`
	ObjectsBytes   int64                 `json:"objects_bytes"`
	WorkspacesBytes int64                `json:"workspaces_bytes"`
	TotalBytes     int64                 `json:"total_bytes"`
	Branches       []branchStorageRow    `json:"branches,omitempty"`
	Snapshots      []snapshotStorageRow  `json:"snapshots,omitempty"`
}

type branchStorageRow struct {
	Name          string `json:"name"`
	SnapshotCount int    `json:"snapshot_count"`
	TotalBytes    int64  `json:"total_bytes"`
}

type snapshotStorageRow struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	BranchName string `json:"branch_name"`
	TotalBytes int64  `json:"total_bytes"`
}

func runStorage(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	avcDir := filepath.Join(projectPath, ".avc")

	// Measure the database file.
	dbSize := fileSize(filepath.Join(avcDir, "avc.db"))

	// Measure the object store.
	objSize, _ := dirSize(filepath.Join(avcDir, "objects"))

	// Measure workspaces.
	wsSize, _ := dirSize(filepath.Join(avcDir, "workspaces"))

	total := dbSize + objSize + wsSize

	// Open DB for branch/snapshot breakdown.
	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectPath)
	if err != nil {
		return err
	}

	summary := storageSummary{
		ProjectName:     proj.Name,
		DatabaseBytes:   dbSize,
		ObjectsBytes:    objSize,
		WorkspacesBytes: wsSize,
		TotalBytes:      total,
	}

	// Build branch-level summary from DB if requested.
	var branchRows []branchStorageRow
	if storageByBranch || storageBySnapshot || jsonOutput {
		branches, _ := store.ListBranches(proj.ID)
		for _, b := range branches {
			snaps, _ := store.ListSnapshotsByBranch(b.ID)
			var branchTotal int64
			for _, s := range snaps {
				branchTotal += s.TotalSize
			}
			branchRows = append(branchRows, branchStorageRow{
				Name:          b.Name,
				SnapshotCount: len(snaps),
				TotalBytes:    branchTotal,
			})
		}
		sort.Slice(branchRows, func(i, j int) bool {
			return branchRows[i].TotalBytes > branchRows[j].TotalBytes
		})
		summary.Branches = branchRows
	}

	// Build per-snapshot list if requested.
	var snapRows []snapshotStorageRow
	if storageBySnapshot || jsonOutput {
		branchNameByID := buildBranchNameMap(store, proj.ID)
		allSnaps, _ := store.ListSnapshots()
		for _, s := range allSnaps {
			snapRows = append(snapRows, snapshotStorageRow{
				ID:         s.ID,
				Label:      s.Label,
				BranchName: branchNameByID[s.BranchID],
				TotalBytes: s.TotalSize,
			})
		}
		// Sort largest first.
		sort.Slice(snapRows, func(i, j int) bool {
			return snapRows[i].TotalBytes > snapRows[j].TotalBytes
		})
		if storageLimit > 0 && len(snapRows) > storageLimit {
			snapRows = snapRows[:storageLimit]
		}
		summary.Snapshots = snapRows
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(summary)
	}

	// ── Human-readable output ────────────────────────────────────────────────
	activeName := branch.GetActiveBranchName(projectPath)
	_ = activeName

	fmt.Printf("%s %s\n%s\n\n", accent("◆ AVC storage:"), cyan(proj.Name), ruler(50))
	fmt.Printf("  %s  %s\n", prop("Database:  "), bold(formatBytes(dbSize)))
	fmt.Printf("  %s  %s\n", prop("Objects:   "), bold(formatBytes(objSize)))
	fmt.Printf("  %s  %s\n", prop("Workspaces:"), bold(formatBytes(wsSize)))
	fmt.Printf("  %s\n", ruler(30))
	fmt.Printf("  %s  %s\n\n", prop("Total:     "), bold(yellow(formatBytes(total))))

	if storageByBranch && len(branchRows) > 0 {
		fmt.Printf("%s\n", accent("Branch snapshot sizes (from DB):"))
		for _, r := range branchRows {
			fmt.Printf("  %-30s %s  (%d snapshot(s))\n",
				r.Name, bold(formatBytes(r.TotalBytes)), r.SnapshotCount)
		}
		fmt.Println()
	}

	if storageBySnapshot && len(snapRows) > 0 {
		fmt.Printf("%s\n", accent(fmt.Sprintf("Top %d snapshots by size:", len(snapRows))))
		for _, r := range snapRows {
			fmt.Printf("  %s  %-40s  %s  %s\n",
				dim(r.ID[:12]+"…"), bold(r.Label), dim(r.BranchName), formatBytes(r.TotalBytes))
		}
		fmt.Println()
	}

	fmt.Printf("%s\n", dim("Run `avc gc` to reclaim storage from deleted snapshots."))
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// dirSize returns the total byte size of all regular files under root.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, statErr := d.Info()
		if statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// fileSize returns the size of a single file, or 0 if it does not exist.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// buildBranchNameMap returns a map of branch ID → branch name for a project.
func buildBranchNameMap(store *db.Store, projectID string) map[string]string {
	m := make(map[string]string)
	branches, err := store.ListBranches(projectID)
	if err != nil {
		return m
	}
	for _, b := range branches {
		m[b.ID] = b.Name
	}
	return m
}
