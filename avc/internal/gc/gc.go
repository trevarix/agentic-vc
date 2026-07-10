// Package gc implements object-store garbage collection for AVC.
//
// The object store at .avc/objects/ is an append-only content-addressed blob
// store. Blobs are never removed during normal snapshot or branch operations.
// Over time, deleted snapshots and deleted branches leave unreferenced blobs
// that waste disk space. gc.Run identifies and optionally removes them.
package gc

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// DefaultGraceWindow is how young an object (or stray temp file) must be to
// be skipped by Run's default grace period. See RunWithGrace.
const DefaultGraceWindow = 15 * time.Minute

// Result summarises a GC run.
type Result struct {
	ScannedObjects int   // total blob files visited in the object store
	DeletedObjects int   // blobs removed (or would be removed in dry-run)
	BytesReclaimed int64 // bytes freed (or would be freed in dry-run)
	SkippedRecent  int   // unreferenced objects younger than the grace window — kept regardless of dry-run
	DryRun         bool  // true when no files were actually deleted
}

// Run scans the object store and removes blobs not referenced by any
// snapshot, using DefaultGraceWindow. See RunWithGrace for why a grace
// period exists at all.
func Run(projectRoot string, dryRun bool) (*Result, error) {
	return RunWithGrace(projectRoot, dryRun, DefaultGraceWindow)
}

// RunWithGrace scans the object store and removes blobs not referenced by
// any snapshot. If dryRun is true, unreferenced blobs are counted and
// reported but not deleted.
//
// Objects (and stray *.tmp files) younger than grace are always skipped,
// regardless of dryRun: StoreObject writes a blob to disk *before* the
// snapshot's DB row is inserted, so a snapshot concurrent with this GC run
// can have an object that looks unreferenced for the brief window between
// those two steps. Without a grace period, GC can race a snapshot in
// progress and delete a blob that snapshot is about to reference — pass
// grace=0 to disable this (e.g. in tests that need exact counts against a
// fully quiescent object store).
//
// The DB connection is opened and closed inside this function — callers
// must not hold an open connection when calling Run/RunWithGrace.
func RunWithGrace(projectRoot string, dryRun bool, grace time.Duration) (*Result, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	live, err := store.LiveHashes()
	store.Close()
	if err != nil {
		return nil, err
	}

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	result := &Result{DryRun: dryRun}
	cutoff := time.Now().Add(-grace)

	// Objects are stored as .avc/objects/<2-hex-shard>/<62-hex-rest>.
	// Reconstruct the full 64-char hash by concatenating shard + filename.
	err = filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		info, statErr := d.Info()
		young := grace > 0 && statErr == nil && info.ModTime().After(cutoff)

		name := filepath.Base(path)
		if strings.HasSuffix(name, ".tmp") {
			// Stray temp file from an interrupted StoreObject write — never
			// referenced by any snapshot, but a *fresh* one may belong to a
			// write still in progress, so it gets the same grace period.
			if young {
				result.SkippedRecent++
				return nil
			}
			if statErr == nil {
				result.BytesReclaimed += info.Size()
			}
			result.DeletedObjects++
			if !dryRun {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}

		shard := filepath.Base(filepath.Dir(path))
		hash := shard + name

		result.ScannedObjects++
		if live[hash] {
			return nil // still referenced — keep it
		}
		if young {
			result.SkippedRecent++
			return nil
		}

		if statErr == nil {
			result.BytesReclaimed += info.Size()
		}
		result.DeletedObjects++

		if !dryRun {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})

	if os.IsNotExist(err) {
		// Objects directory doesn't exist yet — nothing to collect.
		return result, nil
	}
	return result, err
}
