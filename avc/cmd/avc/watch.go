// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/watch"
	"github.com/spf13/cobra"
)

var (
	watchStatus bool
	watchPoll   int
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Continuously checkpoint the project as files change",
	Long: `Runs a foreground daemon that watches the project root (and every active
branch workspace) and automatically snapshots after each burst of changes,
so every state the project passes through is recoverable — whether or not
an agent remembered to snapshot. Press Ctrl+C to stop.

Checkpoints are labeled "auto:watch <what changed>" and pruned first by
retention (default cap: 200 per branch — see [retention]
max_watch_snapshots_per_branch). An idle project generates zero snapshots.

Tune behavior under [watch] in .avc/config.toml:
  debounce_seconds     = 30    quiet period before a checkpoint (default 30)
  min_interval_seconds = 120   minimum gap between checkpoints (default 120)
  include_workspaces   = true  also watch branch workspaces

  avc watch                 start watching (foreground)
  avc watch --status        is a watcher running for this project?
  avc watch --poll 15       poll every 15s instead of using file events
                            (for network filesystems where events are unreliable)`,
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().BoolVar(&watchStatus, "status", false, "Report whether a watcher is running for this project")
	watchCmd.Flags().IntVar(&watchPoll, "poll", 0, "Poll interval in seconds instead of file-event watching (0 = use events)")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	if watchStatus {
		st, err := watch.Status(projectPath)
		if err != nil {
			return fmt.Errorf("watch status: %w", err)
		}
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(st)
		}
		if st.Running {
			fmt.Printf("%s Watcher running (pid %s, last heartbeat %s)\n",
				success("✓"), cyan(fmt.Sprintf("%d", st.PID)),
				dim(time.Unix(st.UpdatedAt, 0).Format("15:04:05")))
		} else {
			fmt.Println(dim("No watcher is running for this project."))
		}
		return nil
	}

	cfg, _ := config.Load(projectPath)
	opts := watch.OptionsFromConfig(cfg)
	if watchPoll > 0 {
		opts.Poll = time.Duration(watchPoll) * time.Second
	}
	opts.Out = os.Stdout

	// Ctrl+C stops the daemon cleanly (pid file removed).
	stop := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println(dim("\nStopping watcher…"))
		close(stop)
	}()

	fmt.Printf("%s %s\n", success("✓ Watching"), cyan(projectPath))
	fmt.Println(dim("  Press Ctrl+C to stop."))
	return watch.Run(projectPath, opts, stop)
}
