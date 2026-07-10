// Package retention applies automatic snapshot-pruning policies.
//
// Policies are configured under [retention] in .avc/config.toml:
//
//	[retention]
//	max_snapshots_per_branch = 100   # keep at most N snapshots per branch
//	max_age_days             = 90    # delete snapshots older than N days
//	auto_gc                  = true  # run gc after pruning
//
// Both rules default to 0 (unlimited), meaning no pruning occurs unless the
// user explicitly enables a policy.
package retention

import (
	"fmt"
	"io"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/gc"
)

// Enforce prunes snapshots on branchID that violate the retention policy.
// It is called from snapshot.Create after each successful snapshot so the
// branch never exceeds the configured limits.
//
// stderr is the writer for informational pruning messages (os.Stderr in
// production, a buffer in tests). Pass io.Discard to silence messages.
//
// If the policy is completely unconfigured (both MaxSnapshotsPerBranch and
// MaxAgeDays are 0), Enforce returns immediately without opening the DB.
func Enforce(projectRoot, branchID string, cfg *config.RetentionConfig, stderr io.Writer) error {
	if cfg == nil || (cfg.MaxSnapshotsPerBranch == 0 && cfg.MaxAgeDays == 0) {
		return nil // no policy configured
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return err
	}
	// Snapshots that are a branch base, tagged, or part of the last merge
	// record must never be auto-pruned — deleting one can corrupt a branch's
	// merge base (spurious conflicts or wrong clean-merges), silently
	// untag a milestone, or invalidate merge history.
	protected, err := store.ProtectedSnapshotIDs(proj.ID)
	if err != nil {
		return err
	}

	// snapshots returned newest-first.
	snaps, err := store.ListSnapshotsByBranch(branchID)
	if err != nil {
		return err
	}

	toDelete := make(map[string]bool)
	skippedProtected := make(map[string]bool)

	// Rule 1 — maximum count (keep the N newest; prune the rest).
	if cfg.MaxSnapshotsPerBranch > 0 && len(snaps) > cfg.MaxSnapshotsPerBranch {
		for _, s := range snaps[cfg.MaxSnapshotsPerBranch:] {
			if protected[s.ID] {
				skippedProtected[s.ID] = true
				continue
			}
			toDelete[s.ID] = true
		}
	}

	// Rule 2 — maximum age.
	if cfg.MaxAgeDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.MaxAgeDays).Unix()
		for _, s := range snaps {
			if s.Timestamp < cutoff {
				if protected[s.ID] {
					skippedProtected[s.ID] = true
					continue
				}
				toDelete[s.ID] = true
			}
		}
	}

	if len(toDelete) == 0 {
		if len(skippedProtected) > 0 {
			fmt.Fprintf(stderr,
				"[avc] Retention policy: %d snapshot(s) would be pruned but are protected (branch base, tagged, or last merge) — kept\n",
				len(skippedProtected),
			)
		}
		return nil
	}

	// Delete pruned snapshots — collect failures instead of swallowing them,
	// so a genuine failure (not "already deleted") is visible to the user.
	var failed []string
	for id := range toDelete {
		if err := store.DeleteSnapshot(id); err != nil {
			failed = append(failed, id)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(stderr, "[avc] warning: failed to prune %d snapshot(s): %v\n", len(failed), failed)
	}

	fmt.Fprintf(stderr,
		"[avc] Pruned %d snapshot(s) on branch '%s' (retention policy)\n",
		len(toDelete), branchID,
	)
	if len(skippedProtected) > 0 {
		fmt.Fprintf(stderr,
			"[avc] Kept %d protected snapshot(s) (branch base, tagged, or last merge)\n",
			len(skippedProtected),
		)
	}

	// Run GC to reclaim object-store space when auto_gc = true in config.
	if cfg.AutoGC {
		_, _ = gc.Run(projectRoot, false)
	}

	return nil
}
