// Package snapshot handles creating and storing project snapshots.
package snapshot

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/db"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/fileutil"
	"github.com/SkillMythOrg/agentic-vc/avc/internal/restore"
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

	snapID := newSnapID()
	now := time.Now().Unix()

	var totalSize int64
	files := make([]*db.File, 0, len(paths))

	for _, absPath := range paths {
		hash, err := fileutil.HashFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("hash file %s: %w", absPath, err)
		}

		data, err := fileutil.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("read file %s: %w", absPath, err)
		}

		if err := restore.StoreObject(projectRoot, hash, data); err != nil {
			return nil, fmt.Errorf("store object %s: %w", absPath, err)
		}

		size := int64(len(data))
		rel, _ := filepath.Rel(projectRoot, absPath)

		files = append(files, &db.File{
			ID:           newFileID(),
			SnapshotID:   snapID,
			RelativePath: filepath.ToSlash(rel),
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
	for _, f := range files {
		if err := store.InsertFile(f); err != nil {
			return nil, fmt.Errorf("insert file record: %w", err)
		}
	}

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
