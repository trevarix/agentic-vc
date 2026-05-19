package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/branch"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/config"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/diff"
	"github.com/spf13/cobra"
)

var branchFromSnapshot string

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Manage branches (agent workspaces)",
	Long: `Branches let agents work in isolation without affecting main.
Each branch is rooted at a base snapshot and accumulates its own snapshot history.`,
}

var branchCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runBranchCreate,
}

var branchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all branches",
	RunE:  runBranchList,
}

var branchSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch the active branch",
	Long: `Updates the active branch in .avc/config.toml.
Does not modify the working directory — use avc restore to roll the project
state to a specific snapshot on the target branch.`,
	Args: cobra.ExactArgs(1),
	RunE: runBranchSwitch,
}

var branchDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runBranchDelete,
}

var branchDiffCmd = &cobra.Command{
	Use:   "diff [branch]",
	Short: "Show cumulative diff from a branch's base snapshot to its HEAD",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runBranchDiff,
}

func init() {
	branchCreateCmd.Flags().StringVar(&branchFromSnapshot, "from", "", "Base snapshot ID (defaults to HEAD of main)")
	branchCmd.AddCommand(branchCreateCmd, branchListCmd, branchSwitchCmd, branchDeleteCmd, branchDiffCmd)
}

func runBranchCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	b, err := branch.Create(projectPath, name, branchFromSnapshot)
	if err != nil {
		return fmt.Errorf("branch create: %w", err)
	}

	// Auto-switch to the new branch — creating a branch means you're about to work on it.
	if err := branch.Switch(projectPath, name); err != nil {
		return fmt.Errorf("branch create succeeded but auto-switch failed: %w", err)
	}

	workspacePath := branch.WorkspacePath(projectPath, b.Name)

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"id":               b.ID,
			"name":             b.Name,
			"base_snapshot_id": b.BaseSnapshotID,
			"created_at":       b.CreatedAt,
			"workspace":        workspacePath,
			"active":           true,
			"success":          true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Branch created and switched:"), cyan(b.Name))
	fmt.Printf("  %s %s\n", prop("Workspace:"), green(workspacePath))
	fmt.Printf("\n%s\n", dim("Direct your agent to work in the workspace directory."))
	return nil
}

func runBranchList(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	branches, err := branch.List(projectPath)
	if err != nil {
		return err
	}

	cfg, _ := config.Load(projectPath)
	activeName := cfg.Branch.Active
	if activeName == "" {
		activeName = "main"
	}

	if jsonOutput {
		type branchJSON struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			BaseSnapshotID string `json:"base_snapshot_id"`
			CreatedAt      int64  `json:"created_at"`
			Active         bool   `json:"active"`
			Workspace      string `json:"workspace"`
		}
		out := make([]branchJSON, len(branches))
		for i, b := range branches {
			out[i] = branchJSON{
				ID:             b.ID,
				Name:           b.Name,
				BaseSnapshotID: b.BaseSnapshotID,
				CreatedAt:      b.CreatedAt,
				Active:         b.Name == activeName,
				Workspace:      branch.WorkspacePath(projectPath, b.Name),
			}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(branches) == 0 {
		fmt.Println("No branches found. Run `avc init` to set up branches.")
		return nil
	}

	for _, b := range branches {
		ts := time.Unix(b.CreatedAt, 0).Format("2006-01-02 15:04")
		if b.Name == activeName {
			fmt.Printf("* %s  %s\n", bold(green(b.Name)), dim(ts))
		} else {
			fmt.Printf("  %s  %s\n", b.Name, dim(ts))
		}
	}
	return nil
}

func runBranchSwitch(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Switch(projectPath, name); err != nil {
		return fmt.Errorf("branch switch: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":    name,
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Switched to branch"), cyan(name))
	fmt.Printf("%s\n", dim("Working directory unchanged — use `avc restore <id>` to restore a snapshot."))
	return nil
}

func runBranchDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if err := branch.Delete(projectPath, name); err != nil {
		return fmt.Errorf("branch delete: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":    name,
			"success": true,
		})
	}

	fmt.Printf("%s %s\n", success("✓ Deleted branch:"), bold(name))
	return nil
}

func runBranchDiff(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	name := branch.GetActiveBranchName(projectPath)
	if len(args) > 0 {
		name = args[0]
	}

	branches, err := branch.List(projectPath)
	if err != nil {
		return err
	}

	var branchID, baseSnapshotID string
	for _, b := range branches {
		if b.Name == name {
			branchID = b.ID
			baseSnapshotID = b.BaseSnapshotID
			break
		}
	}
	if branchID == "" {
		return fmt.Errorf("branch '%s' not found", name)
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	// When no base snapshot exists (branch created before any main snapshot),
	// fall back to the oldest snapshot on the branch as the diff base.
	if baseSnapshotID == "" {
		oldest, err := store.GetOldestSnapshot(branchID)
		if err != nil {
			store.Close()
			return fmt.Errorf("branch '%s' has no snapshots yet", name)
		}
		baseSnapshotID = oldest.ID
	}
	head, err := store.GetHeadSnapshot(branchID)
	store.Close()
	if err != nil {
		return fmt.Errorf("branch '%s' has no snapshots yet", name)
	}

	result, err := diff.Compare(projectPath, baseSnapshotID, head.ID)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	if jsonOutput {
		type fileDiffJSON struct {
			Path         string `json:"path"`
			Type         string `json:"type"`
			OldHash      string `json:"old_hash,omitempty"`
			NewHash      string `json:"new_hash,omitempty"`
			LinesAdded   int    `json:"lines_added"`
			LinesRemoved int    `json:"lines_removed"`
			DiffPreview  string `json:"diff_preview,omitempty"`
		}
		files := make([]fileDiffJSON, len(result.Files))
		for i, f := range result.Files {
			files[i] = fileDiffJSON{
				Path:         f.Path,
				Type:         string(f.Type),
				OldHash:      f.OldHash,
				NewHash:      f.NewHash,
				LinesAdded:   f.LinesAdded,
				LinesRemoved: f.LinesRemoved,
				DiffPreview:  f.DiffPreview,
			}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"branch":        name,
			"from_snapshot": baseSnapshotID,
			"to_snapshot":   head.ID,
			"files":         files,
		})
	}

	fmt.Printf("%s %s\n%s\n\n", accent("◆ Branch diff:"), cyan(name), ruler(50))
	for _, f := range result.Files {
		symbol, pathColor := changeSymbol(string(f.Type))
		added := green(fmt.Sprintf("+%d", f.LinesAdded))
		removed := red(fmt.Sprintf("-%d", f.LinesRemoved))
		fmt.Printf("%s %s (%s %s)\n", symbol, pathColor(f.Path), added, removed)
	}
	if len(result.Files) == 0 {
		fmt.Println("No changes on this branch yet.")
	}
	return nil
}
