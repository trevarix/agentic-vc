# Snapshot Performance Plan

Two targeted optimizations for `avc snapshot` on large codebases.
These are independent and can be implemented in either order.

---

## Optimization 1 — Single read + batched DB inserts

### Problem

`snapshot.go` reads every tracked file twice per snapshot:

1. `fileutil.HashFile` opens the file and streams it through SHA256 → hash string
2. `fileutil.ReadFile` opens the same file again → `[]byte` for storage

On a 1,000-file project this is 2,000 disk reads where 1,000 are sufficient.

Additionally, `store.InsertFile` is called once per file with no explicit transaction.
SQLite auto-commits each call individually, causing one fsync per file.
For 500 files that is 500 fsyncs; a single wrapping transaction reduces this to one.

### Changes

**`avc/internal/fileutil/fileutil.go`**

Add a combined helper that reads the file once and returns both hash and bytes:

```go
// ReadAndHash reads the file at path and returns its contents and SHA256 hex digest.
func ReadAndHash(path string) (data []byte, hash string, err error) {
    data, err = os.ReadFile(path)
    if err != nil {
        return nil, "", err
    }
    sum := sha256.Sum256(data)
    return data, hex.EncodeToString(sum[:]), nil
}
```

`HashFile` and `ReadFile` stay in place — they are used elsewhere and have tests.

**`avc/internal/snapshot/snapshot.go`**

Replace the `HashFile` + `ReadFile` pair with `ReadAndHash`:

```go
// Before (2 reads):
hash, err := fileutil.HashFile(absPath)
data, err := fileutil.ReadFile(absPath)

// After (1 read):
data, hash, err := fileutil.ReadAndHash(absPath)
```

Wrap the file insert loop in an explicit transaction:

```go
tx, err := store.BeginTx()
if err != nil {
    return nil, fmt.Errorf("begin transaction: %w", err)
}
for _, f := range files {
    if err := tx.InsertFile(f); err != nil {
        tx.Rollback()
        return nil, fmt.Errorf("insert file record: %w", err)
    }
}
if err := tx.Commit(); err != nil {
    return nil, fmt.Errorf("commit snapshot: %w", err)
}
```

This requires adding `BeginTx`, `Rollback`, and `Commit` wrappers to
`avc/internal/db/db.go` (or exposing the underlying `*sql.Tx`).

### Expected outcome

- Disk reads cut in half for every snapshot
- DB write time for a 500-file project drops from ~500 fsyncs to 1
- No change to existing behaviour or output

---

## Optimization 2 — Stat cache for unchanged files

### Problem

Every `avc snapshot` re-hashes every tracked file, even files that have not
changed since the last snapshot. For an agent workflow (snapshot before edit →
snapshot after edit), typically 95%+ of files are untouched. Hashing them all
is wasted I/O.

### Design

Store a **stat cache** after each snapshot: a mapping of `relative path →
{mtime_ns, size, hash}` written to `.avc/stat-cache.json`.

On the next snapshot:

1. `os.Stat` the file — nanosecond mtime + size are cheap syscalls
2. If both match the cached values, skip reading the file entirely and reuse the
   cached hash (the object is already stored)
3. If either differs (or the file is new), fall through to `ReadAndHash` and
   update the cache entry

This is the same strategy Git uses for its index (`stat()` before `sha1()`).

### Cache format

`.avc/stat-cache.json`:

```json
{
  "snapshot_id": "snap-abc123",
  "entries": {
    "src/auth.go":      { "mtime_ns": 1712275200000000000, "size": 4096, "hash": "abc..." },
    "config/settings.go": { "mtime_ns": 1712275100000000000, "size": 512,  "hash": "def..." }
  }
}
```

`snapshot_id` records which snapshot the cache was generated from.
On restore, the cache is invalidated (all mtimes change).

### New package: `avc/internal/statcache/statcache.go`

```go
type Entry struct {
    MtimeNs int64  `json:"mtime_ns"`
    Size    int64  `json:"size"`
    Hash    string `json:"hash"`
}

type Cache struct {
    SnapshotID string            `json:"snapshot_id"`
    Entries    map[string]*Entry `json:"entries"`
}

func Load(projectRoot string) (*Cache, error)     // reads .avc/stat-cache.json; returns empty cache on miss
func (c *Cache) Save(projectRoot string) error    // writes .avc/stat-cache.json atomically (write-then-rename)
func (c *Cache) Lookup(rel string, info os.FileInfo) (hash string, hit bool)
func (c *Cache) Set(rel string, info os.FileInfo, hash string)
```

`Lookup` returns `(hash, true)` only when both `mtime_ns` and `size` match.

### Changes to `snapshot.go`

```go
cache, _ := statcache.Load(projectRoot)  // soft failure — miss = full hash

for _, absPath := range paths {
    rel, _ := filepath.Rel(projectRoot, absPath)
    rel = filepath.ToSlash(rel)

    info, err := os.Stat(absPath)
    if err != nil {
        return nil, err
    }

    var hash string
    var data []byte

    if h, hit := cache.Lookup(rel, info); hit {
        hash = h
        // object already stored from a previous snapshot; no read needed
    } else {
        data, hash, err = fileutil.ReadAndHash(absPath)
        if err != nil {
            return nil, err
        }
        if err := restore.StoreObject(projectRoot, hash, data); err != nil {
            return nil, err
        }
        cache.Set(rel, info, hash)
    }

    files = append(files, &db.File{...})
}

cache.SnapshotID = snapID
_ = cache.Save(projectRoot)  // best-effort; a stale cache is safe (just causes a miss)
```

### Invalidation rules

| Event | Cache action |
|-------|-------------|
| `avc snapshot` completes | Rewrite cache with new snapshot ID and updated entries |
| `avc restore` completes | Delete `.avc/stat-cache.json` (all mtimes about to change) |
| `avc init` on existing project | No-op (cache may not exist yet) |
| Cache file corrupt / unreadable | Treat as empty — fall back to full hash, no error |

A stale cache entry causes a **cache miss**, not corruption. The worst outcome
of a stale cache is a redundant re-hash — the snapshot is always correct.

### Expected outcome

- Second and subsequent snapshots on an unchanged project: near-instant (stat
  calls only, no disk reads beyond the walk)
- After a typical agent edit (10–50 files changed out of 1,000+): only changed
  files are read and hashed
- First snapshot on a project, or after `avc restore`: full hash as today

### Tests to add

- `TestReadAndHash` — verifies hash matches `HashFile` output on same file
- `TestStatCacheHit` — mutate nothing, snapshot twice; assert second snapshot reads 0 objects
- `TestStatCacheInvalidatedOnRestore` — verify cache file absent after `avc restore`
- `TestStatCacheMissOnModifiedFile` — touch a file between snapshots; assert it is re-hashed

---

## Implementation order

1. **Optimization 1** first — smaller diff, no new files, tests are straightforward.
   Delivers immediate benefit with minimal risk.
2. **Optimization 2** after — builds on `ReadAndHash` introduced in step 1.
   Adds the `statcache` package and wires it into `snapshot.go`.

Neither optimization changes the public CLI interface, JSON output format, or
object store layout.