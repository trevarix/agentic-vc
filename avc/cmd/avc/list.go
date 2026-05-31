package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	branchpkg "github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

// filter flags shared between listCmd and searchCmd.
var (
	listSearch  string
	listAgent   string
	listSince   string
	listUntil   string
	listChanged string
	listTag     string
	listLimit   int
	listAll     bool // show all branches, not just the active one
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List snapshots for the active branch",
	Long: `Lists snapshots on the active branch, newest first.

Use filters to narrow the results:
  --search "auth refactor"   full-text search on label and notes
  --agent claude             filter by agent name
  --since 2024-06-01         snapshots after this date (YYYY-MM-DD)
  --until 2024-06-30         snapshots before this date (YYYY-MM-DD)
  --changed src/auth.go      snapshots that tracked this file
  --tag stable               snapshots with this tag applied
  --limit 20                 max results (default 50; 0 = unlimited)
  --all                      show snapshots from all branches

Use --json for machine-readable output.`,
	RunE: runList,
}

// searchCmd is an alias for `avc list --search <query>`.
var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search snapshot labels and notes (alias for avc list --search)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		listSearch = args[0]
		return runList(cmd, nil)
	},
}

func init() {
	for _, cmd := range []*cobra.Command{listCmd, searchCmd} {
		cmd.Flags().StringVar(&listSearch, "search", "", "Full-text search on label and notes")
		cmd.Flags().StringVar(&listAgent, "agent", "", "Filter by agent name (substring match)")
		cmd.Flags().StringVar(&listSince, "since", "", "Show snapshots after this date (YYYY-MM-DD)")
		cmd.Flags().StringVar(&listUntil, "until", "", "Show snapshots before this date (YYYY-MM-DD)")
		cmd.Flags().StringVar(&listChanged, "changed", "", "Show snapshots that tracked this file path")
		cmd.Flags().StringVar(&listTag, "tag", "", "Show snapshots with this tag")
		cmd.Flags().IntVar(&listLimit, "limit", 0, "Max results (default 50; 0 = unlimited)")
		cmd.Flags().BoolVar(&listAll, "all", false, "Show snapshots from all branches")
	}
}

func runList(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	cfg, _ := config.Load(projectPath)
	activeName := cfg.Branch.Active
	if activeName == "" {
		activeName = "main"
	}

	// Build filter from flags.
	f := db.SnapshotFilter{
		Query:    listSearch,
		AgentName: listAgent,
		FilePath: listChanged,
		Tag:      listTag,
		Limit:    listLimit,
	}

	// Date parsing.
	if listSince != "" {
		t, err := time.Parse("2006-01-02", listSince)
		if err != nil {
			return fmt.Errorf("--since: invalid date %q (use YYYY-MM-DD)", listSince)
		}
		f.Since = t.Unix()
	}
	if listUntil != "" {
		t, err := time.Parse("2006-01-02", listUntil)
		if err != nil {
			return fmt.Errorf("--until: invalid date %q (use YYYY-MM-DD)", listUntil)
		}
		// Include the full day — advance to end of day.
		f.Until = t.Add(24*time.Hour - time.Second).Unix()
	}

	// Branch scoping: always filter to the active branch unless --all is set.
	// Filters (search, agent, date, etc.) do not widen the scope — use --all for that.
	isFiltered := listSearch != "" || listAgent != "" || listChanged != "" || listTag != "" || listSince != "" || listUntil != ""
	if !listAll {
		branchID, branchErr := branchpkg.GetActiveBranchID(projectPath)
		if branchErr == nil {
			f.BranchID = branchID
		}
	}

	snapshots, err := store.ListSnapshotsFiltered(f)
	if err != nil {
		return err
	}

	if jsonOutput {
		type snapshotJSON struct {
			ID           string `json:"id"`
			Label        string `json:"label"`
			Timestamp    int64  `json:"timestamp"`
			AgentName    string `json:"agent_name"`
			FilesChanged int    `json:"files_changed"`
			TotalSize    int64  `json:"total_size"`
			Notes        string `json:"notes"`
			BranchID     string `json:"branch_id"`
		}
		out := make([]snapshotJSON, len(snapshots))
		for i, s := range snapshots {
			out[i] = snapshotJSON{
				ID:           s.ID,
				Label:        s.Label,
				Timestamp:    s.Timestamp,
				AgentName:    s.AgentName,
				FilesChanged: s.FileCount,
				TotalSize:    s.TotalSize,
				Notes:        s.Notes,
				BranchID:     s.BranchID,
			}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(snapshots) == 0 {
		if isFiltered || listAll {
			fmt.Println("No snapshots match the given filters.")
		} else {
			fmt.Printf("No snapshots on branch %s yet. Run `avc snapshot <label>` to create one.\n",
				bold(activeName))
		}
		return nil
	}

	branchLabel := activeName
	if listAll {
		branchLabel = "all branches"
	}
	fmt.Printf("%s %s\n\n", prop("Branch:"), success(branchLabel))
	fmt.Printf("%s  %s  %s  %s\n",
		accent(fmt.Sprintf("%-20s", "ID")),
		accent(fmt.Sprintf("%-32s", "Label")),
		accent(fmt.Sprintf("%-20s", "Timestamp")),
		accent("Files"))
	fmt.Println(ruler(84))
	for _, s := range snapshots {
		ts := time.Unix(s.Timestamp, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%s  %-32s  %s  %s\n",
			cyan(fmt.Sprintf("%-20s", s.ID)),
			s.Label,
			dim(fmt.Sprintf("%-20s", ts)),
			yellow(fmt.Sprintf("%d", s.FileCount)),
		)
	}
	return nil
}
