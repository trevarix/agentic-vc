// Package hooks executes user-configured shell commands before and after
// AVC operations (snapshot, restore). Pre-hooks abort the calling operation on
// non-zero exit; post-hooks are non-fatal (errors are printed to stderr).
package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const defaultTimeout = 60 * time.Second

// Run executes command in a shell subprocess. Returns an error if:
//   - the command exits with a non-zero status (always checked)
//   - the context deadline is exceeded
//
// AVC environment variables are set for the subprocess:
//
//	AVC_PROJECT_ROOT — absolute path of the project root
//	AVC_SNAPSHOT_ID  — snapshot ID (empty when not yet known, e.g. pre-snapshot)
//	AVC_BRANCH       — currently active branch name
//
// If command is empty, Run is a no-op and returns nil immediately.
func Run(command, projectRoot, snapshotID, branchName string) error {
	if command == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"AVC_PROJECT_ROOT="+projectRoot,
		"AVC_SNAPSHOT_ID="+snapshotID,
		"AVC_BRANCH="+branchName,
	)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook timed out after %s: %s", defaultTimeout, command)
		}
		return fmt.Errorf("hook exited non-zero: %s: %w", command, err)
	}
	return nil
}

// RunPost is like Run but errors are printed to stderr and swallowed — post-hooks
// are non-fatal: a failing post-hook must never block the operation that triggered it.
func RunPost(command, projectRoot, snapshotID, branchName string) {
	if command == "" {
		return
	}
	if err := Run(command, projectRoot, snapshotID, branchName); err != nil {
		fmt.Fprintf(os.Stderr, "[avc] warning: post-hook failed: %v\n", err)
	}
}
