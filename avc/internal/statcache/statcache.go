// Package statcache persists a mapping of file path → {mtime, size, hash} so
// that subsequent snapshots can skip re-reading files that have not changed.
package statcache

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheFile = ".avc/stat-cache.json"

// Entry records the observed mtime, size, and hash of one tracked file.
type Entry struct {
	MtimeNs int64  `json:"mtime_ns"`
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
}

// Cache is the full stat cache for a project.
type Cache struct {
	SnapshotID string            `json:"snapshot_id"`
	Entries    map[string]*Entry `json:"entries"`
}

// Load reads the stat cache from disk. If the file is absent or unreadable an
// empty cache is returned — a miss on every file, which is always safe.
func Load(projectRoot string) (*Cache, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, cacheFile))
	if err != nil {
		return empty(), nil
	}
	c := &Cache{}
	if err := json.Unmarshal(data, c); err != nil {
		return empty(), nil
	}
	if c.Entries == nil {
		c.Entries = make(map[string]*Entry)
	}
	return c, nil
}

// Save writes the cache to disk atomically (write to a temp file, then rename).
// Errors are non-fatal — a missing or stale cache causes misses, not corruption.
func (c *Cache) Save(projectRoot string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := filepath.Join(projectRoot, cacheFile+".tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(projectRoot, cacheFile))
}

// Lookup returns the cached hash for rel if mtime_ns and size both match info.
// Returns ("", false) on any mismatch — the caller must re-hash the file.
func (c *Cache) Lookup(rel string, info os.FileInfo) (hash string, hit bool) {
	e, ok := c.Entries[rel]
	if !ok {
		return "", false
	}
	if e.MtimeNs == info.ModTime().UnixNano() && e.Size == info.Size() {
		return e.Hash, true
	}
	return "", false
}

// Set records a new or updated entry for rel.
func (c *Cache) Set(rel string, info os.FileInfo, hash string) {
	c.Entries[rel] = &Entry{
		MtimeNs: info.ModTime().UnixNano(),
		Size:    info.Size(),
		Hash:    hash,
	}
}

// Invalidate deletes the stat cache file. Must be called after avc restore
// because WriteFile changes every restored file's mtime, making all cached
// entries stale.
func Invalidate(projectRoot string) {
	_ = os.Remove(filepath.Join(projectRoot, cacheFile))
}

// Empty returns a new cache with no entries. Used when the stat cache should
// not be consulted (e.g. when snapshotting a workspace rather than the project root).
func Empty() *Cache {
	return empty()
}

func empty() *Cache {
	return &Cache{Entries: make(map[string]*Entry)}
}
