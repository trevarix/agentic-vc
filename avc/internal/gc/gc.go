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

	"github.com/trevarix/agentic-vc/avc/internal/db"
)

// Result summarises a GC run.
type Result struct {
	ScannedObjects int   // total blob files visited in the object store
	DeletedObjects int   // blobs removed (or would be removed in dry-run)
	BytesReclaimed int64 // bytes freed (or would be freed in dry-run)
	DryRun         bool  // true when no files were actually deleted
}

// Run scans the object store and removes blobs not referenced by any snapshot.
// If dryRun is true, unreferenced blobs are counted and reported but not deleted.
// The DB connection is opened and closed inside this function — callers must not
// hold an open connection when calling Run.
func Run(projectRoot string, dryRun bool) (*Result, error) {
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

	// Objects are stored as .avc/objects/<2-hex-shard>/<62-hex-rest>.
	// Reconstruct the full 64-char hash by concatenating shard + filename.
	err = filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		shard := filepath.Base(filepath.Dir(path))
		name := filepath.Base(path)
		hash := shard + name

		result.ScannedObjects++
		if live[hash] {
			return nil // still referenced — keep it
		}

		info, statErr := d.Info()
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
