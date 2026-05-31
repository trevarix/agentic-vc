package avc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the diff result cache",
	Long: `AVC caches computed diffs in the database to speed up repeated queries.
Use 'avc cache stats' to see cache size, or 'avc cache clear' to reset it.`,
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete all cached diff results",
	RunE:  runCacheClear,
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show diff cache statistics (row count, oldest entry)",
	RunE:  runCacheStats,
}

func init() {
	cacheCmd.AddCommand(cacheClearCmd, cacheStatsCmd)
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.ClearDiffCache(); err != nil {
		return fmt.Errorf("clear cache: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"cleared": true,
			"success": true,
		})
	}

	fmt.Printf("%s\n", success("✓ Diff cache cleared."))
	return nil
}

func runCacheStats(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	store, err := db.Open(projectPath)
	if err != nil {
		return err
	}
	defer store.Close()

	count, oldest, err := store.DiffCacheStats()
	if err != nil {
		return fmt.Errorf("cache stats: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"cached_rows": count,
			"oldest_at":   oldest,
		})
	}

	if count == 0 {
		fmt.Printf("%s\n", dim("Diff cache is empty."))
		return nil
	}

	oldestStr := dim("—")
	if oldest > 0 {
		oldestStr = dim(time.Unix(oldest, 0).Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("%s %s\n", prop("Cached diff rows:"), yellow(fmt.Sprintf("%d", count)))
	fmt.Printf("%s %s\n", prop("Oldest entry:    "), oldestStr)
	fmt.Printf("\nRun %s to clear all cached results.\n", cyan("avc cache clear"))
	return nil
}
