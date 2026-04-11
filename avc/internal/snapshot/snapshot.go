// Package snapshot handles creating and storing project snapshots.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/statcache"
)

// Result is returned by Create after a successful snapshot.
type Result struct {
	ID        string
	Label     string
	Timestamp int64
	AgentName string
	Notes     string
	FileCount int
	TotalSize int64
}

// Create walks the project directory, hashes all tracked files, and persists
// a snapshot record to the database. Returns the snapshot result on success.
//
// Each file is read at most once (ReadAndHash). Files whose mtime and size
// match the stat cache from the previous snapshot are not read at all — the
// cached hash is reused and the object store write is skipped.
// All file DB records are inserted in a single transaction.
func Create(projectRoot, label, agentName, notes string) (*Result, error) {
	ignore, err := fileutil.LoadIgnoreRules(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load ignore rules: %w", err)
	}

	paths, err := fileutil.WalkProject(projectRoot, ignore)
	if err != nil {
		return nil, fmt.Errorf("walk project: %w", err)
	}

	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	project, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized (run `avc init`): %w", err)
	}

	// Load stat cache — a missing or corrupt cache returns an empty one (safe miss).
	cache, _ := statcache.Load(projectRoot)

	snapID := newSnapID()
	now := time.Now().Unix()

	var totalSize int64
	files := make([]*db.File, 0, len(paths))

	for _, absPath := range paths {
		rel, _ := filepath.Rel(projectRoot, absPath)
		rel = filepath.ToSlash(rel)

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat file %s: %w", absPath, err)
		}

		var hash string
		var size int64

		if h, hit := cache.Lookup(rel, info); hit {
			// File unchanged since the last snapshot — object already stored.
			hash = h
			size = info.Size()
		} else {
			// File is new or modified — read once, derive hash from bytes.
			data, h, err := fileutil.ReadAndHash(absPath)
			if err != nil {
				return nil, fmt.Errorf("read file %s: %w", absPath, err)
			}
			if err := restore.StoreObject(projectRoot, h, data); err != nil {
				return nil, fmt.Errorf("store object %s: %w", absPath, err)
			}
			hash = h
			size = int64(len(data))
			cache.Set(rel, info, hash)
		}

		files = append(files, &db.File{
			ID:           newFileID(),
			SnapshotID:   snapID,
			RelativePath: rel,
			FileHash:     hash,
			FileSize:     size,
		})
		totalSize += size
	}

	snap := &db.Snapshot{
		ID:        snapID,
		ProjectID: project.ID,
		Timestamp: now,
		Label:     label,
		AgentName: agentName,
		Notes:     notes,
		FileCount: len(files),
		TotalSize: totalSize,
	}

	if err := store.InsertSnapshot(snap); err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}
	if err := store.InsertFilesBatch(files); err != nil {
		return nil, fmt.Errorf("insert files: %w", err)
	}

	// Persist updated cache — best-effort; a stale cache only causes misses.
	cache.SnapshotID = snapID
	_ = cache.Save(projectRoot)

	return &Result{
		ID:        snapID,
		Label:     label,
		Timestamp: now,
		AgentName: agentName,
		Notes:     notes,
		FileCount: len(files),
		TotalSize: totalSize,
	}, nil
}
