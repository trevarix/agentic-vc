// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package db manages all SQLite operations for AVC.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// normPath returns a consistently-cased, clean path for use as a DB key.
// On Windows, the volume name (drive letter) is uppercased so C:\ and c:\
// are treated identically when the VSCode extension and CLI compare paths.
func normPath(p string) string {
	p = filepath.Clean(p)
	if vol := filepath.VolumeName(p); vol != "" {
		p = strings.ToUpper(vol) + p[len(vol):]
	}
	return p
}

const dbFile = ".avc/avc.db"

// Store wraps the SQLite connection and provides typed query methods.
type Store struct {
	db *sql.DB
}

// Project represents a row in the projects table.
type Project struct {
	ID        string
	Path      string
	Name      string
	CreatedAt int64
}

// Branch represents an agent workspace.
type Branch struct {
	ID             string
	Name           string
	ProjectID      string
	BaseSnapshotID string // empty string for main (no base snapshot)
	CreatedAt      int64
	Status         string // "active" | "merged" | "abandoned"
}

// Snapshot represents a row in the snapshots table.
type Snapshot struct {
	ID        string
	ProjectID string
	Timestamp int64
	Label     string
	AgentName string
	Notes     string
	FileCount int
	TotalSize int64
	BranchID  string // empty string when branch_id is NULL (pre-Phase-4 rows)
}

// File represents a row in the files table.
type File struct {
	ID           string
	SnapshotID   string
	RelativePath string
	FileHash     string
	FileSize     int64
	FileMode     uint32 // Unix permission bits (e.g. 0644, 0755); 0 = not recorded (pre-mode-tracking row, or platform without meaningful modes) — restore falls back to 0644
}

// Merge represents one merge operation (branch → main).
type Merge struct {
	ID             string
	ProjectID      string
	BranchID       string
	BaseSnapshotID string // agentBranch.BaseSnapshotID at merge time
	MainSnapshotID string // auto-snapshot of main created just before merge
	HeadSnapshotID string // HEAD of agent branch at merge time
	Status         string // "in_progress" | "completed" | "conflicts" | "aborted"
	StartedAt      int64
	FinishedAt     int64 // 0 until completed/aborted
}

// MergeFile records the per-file decision for one merge.
type MergeFile struct {
	ID           string
	MergeID      string
	RelativePath string
	Decision     string // "clean" | "conflict" | "skip"
	BaseHash     string
	MainHash     string
	BranchHash   string
}

// DiffCache represents a row in the diffs table.
type DiffCache struct {
	ID             string
	FromSnapshotID string
	ToSnapshotID   string
	FilePath       string
	DiffType       string
	OldHash        string
	NewHash        string
	ChangeSummary  string
}

// Open opens (or creates) the AVC SQLite database for the given project root
// and applies all schema migrations.
func Open(projectRoot string) (*Store, error) {
	path := filepath.Join(projectRoot, dbFile)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Apply pragmas before migrations so every subsequent query benefits from them.
	// busy_timeout MUST be set first: switching journal_mode itself briefly
	// needs exclusive access, and without a timeout already in effect that
	// very first pragma can fail with "database is locked" under contention.
	pragmas := []string{
		"PRAGMA busy_timeout=5000",  // writers wait up to 5s instead of failing on SQLITE_BUSY
		"PRAGMA journal_mode=WAL",   // concurrent readers during writes
		"PRAGMA synchronous=NORMAL", // safe + faster than FULL
		"PRAGMA cache_size=-65536",  // 64 MB page cache
		"PRAGMA foreign_keys=ON",    // enforce FK constraints
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// InitProject creates the .avc directory, initializes the schema, and inserts
// a project record. Returns the project (existing or newly created).
func InitProject(projectRoot string) (*Project, error) {
	avcDir := filepath.Join(projectRoot, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		return nil, fmt.Errorf("create .avc directory: %w", err)
	}

	store, err := Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	normed := normPath(projectRoot)
	project := &Project{
		ID:        newID("proj"),
		Path:      normed,
		Name:      filepath.Base(normed),
		CreatedAt: nowUnix(),
	}

	_, err = store.db.Exec(
		`INSERT OR IGNORE INTO projects (id, path, name, created_at) VALUES (?, ?, ?, ?)`,
		project.ID, project.Path, project.Name, project.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	return store.GetProject(normed)
}

// migrate creates all tables if absent and applies incremental schema changes
// idempotently. Safe to call on every Open.
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id         TEXT PRIMARY KEY,
		path       TEXT UNIQUE,
		name       TEXT,
		created_at INTEGER
	);

	CREATE TABLE IF NOT EXISTS branches (
		id               TEXT PRIMARY KEY,
		name             TEXT NOT NULL,
		project_id       TEXT NOT NULL,
		base_snapshot_id TEXT,
		created_at       INTEGER NOT NULL,
		UNIQUE (project_id, name),
		FOREIGN KEY (project_id) REFERENCES projects(id)
	);

	CREATE TABLE IF NOT EXISTS snapshots (
		id         TEXT PRIMARY KEY,
		project_id TEXT,
		timestamp  INTEGER,
		label      TEXT,
		agent_name TEXT,
		notes      TEXT,
		file_count INTEGER,
		total_size INTEGER,
		FOREIGN KEY (project_id) REFERENCES projects(id)
	);

	CREATE TABLE IF NOT EXISTS files (
		id            TEXT PRIMARY KEY,
		snapshot_id   TEXT,
		relative_path TEXT,
		file_hash     TEXT,
		file_size     INTEGER,
		FOREIGN KEY (snapshot_id) REFERENCES snapshots(id)
	);

	CREATE TABLE IF NOT EXISTS diffs (
		id               TEXT PRIMARY KEY,
		from_snapshot_id TEXT,
		to_snapshot_id   TEXT,
		file_path        TEXT,
		diff_type        TEXT,
		old_hash         TEXT,
		new_hash         TEXT,
		change_summary   TEXT,
		FOREIGN KEY (from_snapshot_id) REFERENCES snapshots(id),
		FOREIGN KEY (to_snapshot_id)   REFERENCES snapshots(id)
	);

	CREATE TABLE IF NOT EXISTS merges (
		id               TEXT PRIMARY KEY,
		project_id       TEXT NOT NULL,
		branch_id        TEXT NOT NULL,
		base_snapshot_id TEXT NOT NULL,
		main_snapshot_id TEXT NOT NULL,
		head_snapshot_id TEXT NOT NULL,
		status           TEXT NOT NULL DEFAULT 'in_progress',
		started_at       INTEGER NOT NULL,
		finished_at      INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (project_id) REFERENCES projects(id),
		FOREIGN KEY (branch_id)  REFERENCES branches(id)
	);

	CREATE TABLE IF NOT EXISTS merge_files (
		id            TEXT PRIMARY KEY,
		merge_id      TEXT NOT NULL,
		relative_path TEXT NOT NULL,
		decision      TEXT NOT NULL,
		base_hash     TEXT NOT NULL DEFAULT '',
		main_hash     TEXT NOT NULL DEFAULT '',
		branch_hash   TEXT NOT NULL DEFAULT '',
		FOREIGN KEY (merge_id) REFERENCES merges(id)
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Phase 4: add branch_id to snapshots (idempotent — SQLite returns
	// "duplicate column name" if it already exists, which we ignore; any
	// other failure, e.g. a locked or full-disk database, must propagate).
	if err := execIgnoreDuplicateColumn(s.db, `ALTER TABLE snapshots ADD COLUMN branch_id TEXT REFERENCES branches(id)`); err != nil {
		return fmt.Errorf("add snapshots.branch_id: %w", err)
	}

	// Phase 7.1: branch lifecycle status column.
	if err := execIgnoreDuplicateColumn(s.db, `ALTER TABLE branches ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
		return fmt.Errorf("add branches.status: %w", err)
	}

	// Lifecycle hardening: file_mode preserves Unix permission bits (notably
	// the executable bit) across snapshot/restore. 0 default means "not
	// recorded" for rows written before this column existed.
	if err := execIgnoreDuplicateColumn(s.db, `ALTER TABLE files ADD COLUMN file_mode INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add files.file_mode: %w", err)
	}

	// Phase 7.3: project_state — stores the active branch name inside the DB
	// (eliminates the config.toml race condition for concurrent writers).
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS project_state (
			project_id    TEXT PRIMARY KEY,
			active_branch TEXT NOT NULL DEFAULT 'main',
			updated_at    INTEGER NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id)
		)`); err != nil {
		return fmt.Errorf("create project_state table: %w", err)
	}

	// Phase 5.2: snapshot_tags table for machine-readable milestone markers.
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS snapshot_tags (
			snapshot_id TEXT NOT NULL,
			tag         TEXT NOT NULL,
			created_at  INTEGER NOT NULL,
			PRIMARY KEY (snapshot_id, tag),
			FOREIGN KEY (snapshot_id) REFERENCES snapshots(id)
		)`)
	if err != nil {
		return fmt.Errorf("create snapshot_tags table: %w", err)
	}

	// Phase 5.3: add computed_at to the diffs cache table (tracks when each
	// cached diff was computed; enables TTL-based invalidation).
	if err := execIgnoreDuplicateColumn(s.db, `ALTER TABLE diffs ADD COLUMN computed_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add diffs.computed_at: %w", err)
	}

	// Phase 1 improvement: query indexes for hot paths.
	// All idempotent via IF NOT EXISTS — safe to run on every Open.
	indexes := []string{
		// GetSnapshotFiles: WHERE snapshot_id = ?
		`CREATE INDEX IF NOT EXISTS idx_files_snapshot_id ON files(snapshot_id)`,
		// GetFileVersions (annotate): WHERE relative_path = ?
		`CREATE INDEX IF NOT EXISTS idx_files_path ON files(relative_path)`,
		// ListSnapshotsByBranch: WHERE branch_id = ? ORDER BY timestamp DESC
		`CREATE INDEX IF NOT EXISTS idx_snapshots_branch_ts ON snapshots(branch_id, timestamp DESC)`,
		// GetMergeFiles: WHERE merge_id = ?
		`CREATE INDEX IF NOT EXISTS idx_merge_files_merge_id ON merge_files(merge_id)`,
		// GetBranchByName: WHERE project_id = ? AND name = ?
		`CREATE INDEX IF NOT EXISTS idx_branches_project_name ON branches(project_id, name)`,
		// ListSnapshotsByTag: WHERE tag = ?
		`CREATE INDEX IF NOT EXISTS idx_snapshot_tags_tag ON snapshot_tags(tag)`,
	}
	for _, idx := range indexes {
		if _, err := s.db.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	return nil
}

// execIgnoreDuplicateColumn runs an idempotent `ALTER TABLE ... ADD COLUMN`
// migration statement. SQLite has no `IF NOT EXISTS` for columns, so a
// column that already exists returns an error containing "duplicate column
// name" — that specific case is expected on every Open() after the first and
// is ignored. Any other error (locked database, disk full, syntax error, ...)
// propagates, so a real migration failure is never mistaken for "already applied".
func execIgnoreDuplicateColumn(db *sql.DB, stmt string) error {
	_, err := db.Exec(stmt)
	if err == nil || strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return err
}

// ─── Project ─────────────────────────────────────────────────────────────────

// GetProject returns the project record for the given root path.
func (s *Store) GetProject(projectRoot string) (*Project, error) {
	row := s.db.QueryRow(
		`SELECT id, path, name, created_at FROM projects WHERE path = ?`, normPath(projectRoot),
	)
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Path, &p.Name, &p.CreatedAt); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	return p, nil
}

// ─── Branch ──────────────────────────────────────────────────────────────────

const branchSelectCols = `id, name, project_id, COALESCE(base_snapshot_id, ''), created_at, COALESCE(status, 'active')`

func scanBranch(row interface{ Scan(...any) error }) (*Branch, error) {
	b := &Branch{}
	err := row.Scan(&b.ID, &b.Name, &b.ProjectID, &b.BaseSnapshotID, &b.CreatedAt, &b.Status)
	return b, err
}

// InsertBranch persists a new branch record.
func (s *Store) InsertBranch(b *Branch) error {
	status := b.Status
	if status == "" {
		status = "active"
	}
	_, err := s.db.Exec(
		`INSERT INTO branches (id, name, project_id, base_snapshot_id, created_at, status)
		 VALUES (?, ?, ?, NULLIF(?, ''), ?, ?)`,
		b.ID, b.Name, b.ProjectID, b.BaseSnapshotID, b.CreatedAt, status,
	)
	return err
}

// GetBranchByName returns a branch by project and name.
func (s *Store) GetBranchByName(projectID, name string) (*Branch, error) {
	row := s.db.QueryRow(
		`SELECT `+branchSelectCols+` FROM branches WHERE project_id = ? AND name = ?`,
		projectID, name,
	)
	b, err := scanBranch(row)
	if err != nil {
		return nil, fmt.Errorf("branch '%s' not found", name)
	}
	return b, nil
}

// ListBranches returns all branches for a project ordered by creation time.
func (s *Store) ListBranches(projectID string) ([]*Branch, error) {
	rows, err := s.db.Query(
		`SELECT `+branchSelectCols+` FROM branches WHERE project_id = ? ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []*Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return branches, rows.Err()
}

// ListBranchesByStatus returns all branches for a project with a given status.
// Pass "" to list all statuses (same as ListBranches).
func (s *Store) ListBranchesByStatus(projectID, status string) ([]*Branch, error) {
	var q string
	var args []any
	if status == "" {
		q = `SELECT ` + branchSelectCols + ` FROM branches WHERE project_id = ? ORDER BY created_at ASC`
		args = []any{projectID}
	} else {
		q = `SELECT ` + branchSelectCols + ` FROM branches WHERE project_id = ? AND COALESCE(status,'active') = ? ORDER BY created_at ASC`
		args = []any{projectID, status}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []*Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return branches, rows.Err()
}

// SetBranchStatus updates the lifecycle status of a branch.
// Valid values: "active", "merged", "abandoned".
func (s *Store) SetBranchStatus(branchID, status string) error {
	_, err := s.db.Exec(`UPDATE branches SET status = ? WHERE id = ?`, status, branchID)
	return err
}

// RenameBranch changes the name of a branch record in the DB.
func (s *Store) RenameBranch(branchID, newName string) error {
	_, err := s.db.Exec(`UPDATE branches SET name = ? WHERE id = ?`, newName, branchID)
	return err
}

// DeleteBranch removes a branch record. Callers should call
// DeleteSnapshotsByBranch first unless they intend to keep the history.
func (s *Store) DeleteBranch(id string) error {
	_, err := s.db.Exec(`DELETE FROM branches WHERE id = ?`, id)
	return err
}

// DeleteSnapshotsByBranch removes all snapshot and file records whose
// branch_id matches the given branch. Object blobs in the object store are
// NOT removed — call gc.Run afterwards to reclaim disk space.
func (s *Store) DeleteSnapshotsByBranch(branchID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Delete files first to satisfy the FK constraint on files.snapshot_id.
	if _, err := tx.Exec(
		`DELETE FROM files WHERE snapshot_id IN
		 (SELECT id FROM snapshots WHERE branch_id = ?)`, branchID,
	); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM snapshots WHERE branch_id = ?`, branchID); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DetachSnapshotsFromBranch sets branch_id = NULL on all snapshots belonging
// to branchID. Used when a branch is deleted with --keep-history so that the
// FK constraint on branches(id) is satisfied while the snapshot rows are retained.
func (s *Store) DetachSnapshotsFromBranch(branchID string) error {
	_, err := s.db.Exec(
		`UPDATE snapshots SET branch_id = NULL WHERE branch_id = ?`, branchID,
	)
	return err
}

// ─── Project State ───────────────────────────────────────────────────────────

// GetActiveBranch returns the name of the currently active branch from the
// project_state table. Returns "main" if no row exists yet.
func (s *Store) GetActiveBranch(projectID string) (string, error) {
	var name string
	err := s.db.QueryRow(
		`SELECT active_branch FROM project_state WHERE project_id = ?`, projectID,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		// No row yet — default to main.
		return "main", nil
	}
	if err != nil {
		// A real failure (locked DB, I/O error, ...) must propagate: silently
		// returning "main" here would retarget snapshots/restores to the
		// wrong branch on a transient error instead of surfacing it.
		return "", err
	}
	return name, nil
}

// SetActiveBranch upserts the active branch name into project_state.
func (s *Store) SetActiveBranch(projectID, name string) error {
	_, err := s.db.Exec(
		`INSERT INTO project_state (project_id, active_branch, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(project_id) DO UPDATE SET
		     active_branch = excluded.active_branch,
		     updated_at    = excluded.updated_at`,
		projectID, name, nowUnix(),
	)
	return err
}

// LiveHashes returns the set of all file_hash values currently referenced by
// any snapshot in the database. Used by GC to identify safe-to-delete objects.
func (s *Store) LiveHashes() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT file_hash FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	live := make(map[string]bool)
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		live[h] = true
	}
	return live, rows.Err()
}

// EnsureMainBranch creates the main branch for projectID if it does not exist,
// then backfills any snapshots with NULL branch_id to that branch.
// Safe to call multiple times — fully idempotent.
func (s *Store) EnsureMainBranch(projectID string) (*Branch, error) {
	row := s.db.QueryRow(
		`SELECT `+branchSelectCols+` FROM branches WHERE project_id = ? AND name = 'main'`,
		projectID,
	)
	b, err := scanBranch(row)
	if err != nil {
		// Main branch absent — create it.
		b = &Branch{
			ID:        newID("branch"),
			Name:      "main",
			ProjectID: projectID,
			CreatedAt: nowUnix(),
			Status:    "active",
		}
		if _, err := s.db.Exec(
			`INSERT OR IGNORE INTO branches (id, name, project_id, base_snapshot_id, created_at, status)
			 VALUES (?, 'main', ?, NULL, ?, 'active')`,
			b.ID, b.ProjectID, b.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("create main branch: %w", err)
		}
		// Re-fetch in case INSERT OR IGNORE hit an existing row.
		row = s.db.QueryRow(
			`SELECT `+branchSelectCols+` FROM branches WHERE project_id = ? AND name = 'main'`,
			projectID,
		)
		if b, err = scanBranch(row); err != nil {
			return nil, fmt.Errorf("fetch main branch: %w", err)
		}
	}

	// Backfill pre-Phase-4 snapshots that have no branch_id.
	if _, err := s.db.Exec(
		`UPDATE snapshots SET branch_id = ? WHERE branch_id IS NULL`, b.ID,
	); err != nil {
		return nil, fmt.Errorf("backfill snapshots: %w", err)
	}
	return b, nil
}

// ─── Snapshot ────────────────────────────────────────────────────────────────

// InsertSnapshotWithFiles persists the snapshot row and all of its file rows
// in a single transaction, so a crash between the two writes can never leave
// a snapshot with zero file rows (which a later restore would interpret as
// "delete every tracked file").
func (s *Store) InsertSnapshotWithFiles(snap *Snapshot, files []*File) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO snapshots
		 (id, project_id, timestamp, label, agent_name, notes, file_count, total_size, branch_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		snap.ID, snap.ProjectID, snap.Timestamp, snap.Label,
		snap.AgentName, snap.Notes, snap.FileCount, snap.TotalSize, snap.BranchID,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("insert snapshot: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO files (id, snapshot_id, relative_path, file_hash, file_size, file_mode) VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, f := range files {
		if _, err := stmt.Exec(f.ID, f.SnapshotID, f.RelativePath, f.FileHash, f.FileSize, f.FileMode); err != nil {
			stmt.Close()
			tx.Rollback()
			return fmt.Errorf("insert file %s: %w", f.RelativePath, err)
		}
	}
	stmt.Close()

	return tx.Commit()
}

const snapshotSelectCols = `id, project_id, timestamp, label, agent_name, notes, file_count, total_size,
	COALESCE(branch_id, '')`

func scanSnapshot(row interface{ Scan(...any) error }) (*Snapshot, error) {
	snap := &Snapshot{}
	err := row.Scan(
		&snap.ID, &snap.ProjectID, &snap.Timestamp, &snap.Label,
		&snap.AgentName, &snap.Notes, &snap.FileCount, &snap.TotalSize, &snap.BranchID,
	)
	return snap, err
}

// ListSnapshots returns all snapshots, newest first (no branch filter).
// Used internally by restore and similar operations that need full visibility.
func (s *Store) ListSnapshots() ([]*Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT ` + snapshotSelectCols + ` FROM snapshots ORDER BY timestamp DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// ListSnapshotsByBranch returns snapshots for a specific branch, newest first.
func (s *Store) ListSnapshotsByBranch(branchID string) ([]*Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT `+snapshotSelectCols+` FROM snapshots WHERE branch_id = ? ORDER BY timestamp DESC`,
		branchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// SnapshotFilter holds optional predicates for ListSnapshotsFiltered.
// Zero values mean "no filter" for that field.
type SnapshotFilter struct {
	BranchID  string // exact match; empty = all branches
	AgentName string // LIKE match (case-insensitive prefix/substring)
	Query     string // full-text search on label + notes (LIKE %query%)
	Tag       string // only snapshots with this tag
	Since     int64  // Unix timestamp lower bound (inclusive)
	Until     int64  // Unix timestamp upper bound (inclusive)
	FilePath  string // only snapshots that tracked this relative path
	Limit     int    // 0 = use default (50); negative = unlimited
}

// ListSnapshotsFiltered returns snapshots matching all non-zero filter fields,
// newest first. It is the engine behind `avc list --search`, `--agent`, etc.
func (s *Store) ListSnapshotsFiltered(f SnapshotFilter) ([]*Snapshot, error) {
	var conditions []string
	var args []any

	if f.BranchID != "" {
		conditions = append(conditions, "s.branch_id = ?")
		args = append(args, f.BranchID)
	}
	if f.AgentName != "" {
		conditions = append(conditions, "s.agent_name LIKE ?")
		args = append(args, "%"+f.AgentName+"%")
	}
	if f.Query != "" {
		conditions = append(conditions, "(s.label LIKE ? OR s.notes LIKE ?)")
		args = append(args, "%"+f.Query+"%", "%"+f.Query+"%")
	}
	if f.Since > 0 {
		conditions = append(conditions, "s.timestamp >= ?")
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		conditions = append(conditions, "s.timestamp <= ?")
		args = append(args, f.Until)
	}
	if f.FilePath != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM files WHERE snapshot_id = s.id AND relative_path = ?)")
		args = append(args, f.FilePath)
	}
	if f.Tag != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM snapshot_tags WHERE snapshot_id = s.id AND tag = ?)")
		args = append(args, f.Tag)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := f.Limit
	if limit == 0 {
		limit = 50 // default
	}
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}

	q := fmt.Sprintf(
		`SELECT %s FROM snapshots s %s ORDER BY s.timestamp DESC, s.rowid DESC %s`,
		snapshotSelectColsAliased, where, limitClause,
	)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// snapshotSelectColsAliased is snapshotSelectCols with the "s." table alias
// used by ListSnapshotsFiltered (which joins via EXISTS subqueries).
const snapshotSelectColsAliased = `s.id, s.project_id, s.timestamp, s.label, s.agent_name, s.notes, s.file_count, s.total_size,
	COALESCE(s.branch_id, '')`

// ─── Snapshot tags ────────────────────────────────────────────────────────────

// TagSnapshot applies tag to a snapshot. Idempotent — applying the same tag
// twice is a no-op (PRIMARY KEY constraint).
func (s *Store) TagSnapshot(snapshotID, tag string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO snapshot_tags (snapshot_id, tag, created_at) VALUES (?, ?, ?)`,
		snapshotID, tag, nowUnix(),
	)
	return err
}

// UntagSnapshot removes a tag from a snapshot. Returns nil if the tag was not set.
func (s *Store) UntagSnapshot(snapshotID, tag string) error {
	_, err := s.db.Exec(
		`DELETE FROM snapshot_tags WHERE snapshot_id = ? AND tag = ?`,
		snapshotID, tag,
	)
	return err
}

// GetSnapshotTags returns all tags applied to a snapshot, in creation order.
func (s *Store) GetSnapshotTags(snapshotID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT tag FROM snapshot_tags WHERE snapshot_id = ? ORDER BY created_at ASC`,
		snapshotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListSnapshotsByTag returns all snapshots carrying the given tag, newest first.
func (s *Store) ListSnapshotsByTag(tag string) ([]*Snapshot, error) {
	return s.ListSnapshotsFiltered(SnapshotFilter{Tag: tag, Limit: -1})
}

// ─── Diff cache management ────────────────────────────────────────────────────

// ClearDiffCache deletes all cached diff rows.
func (s *Store) ClearDiffCache() error {
	_, err := s.db.Exec(`DELETE FROM diffs`)
	return err
}

// DiffCacheStats returns the count of cached diff rows and the oldest computed_at timestamp.
func (s *Store) DiffCacheStats() (count int, oldest int64, err error) {
	row := s.db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(computed_at), 0) FROM diffs`)
	err = row.Scan(&count, &oldest)
	return
}

// GetSnapshot returns a single snapshot by ID.
func (s *Store) GetSnapshot(id string) (*Snapshot, error) {
	row := s.db.QueryRow(
		`SELECT `+snapshotSelectCols+` FROM snapshots WHERE id = ?`, id,
	)
	snap, err := scanSnapshot(row)
	if err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", id)
	}
	return snap, nil
}

// GetHeadSnapshot returns the most recent snapshot on a branch.
func (s *Store) GetHeadSnapshot(branchID string) (*Snapshot, error) {
	row := s.db.QueryRow(
		`SELECT `+snapshotSelectCols+
			// rowid is the SQLite auto-assigned insertion order — used as a
			// tiebreaker so two snapshots created in the same Unix second
			// (e.g. pre-merge and post-merge) are ordered correctly.
			` FROM snapshots WHERE branch_id = ? ORDER BY timestamp DESC, rowid DESC LIMIT 1`,
		branchID,
	)
	snap, err := scanSnapshot(row)
	if err != nil {
		return nil, fmt.Errorf("no snapshots on this branch")
	}
	return snap, nil
}

// GetOldestSnapshot returns the earliest snapshot on a branch (the "before" snapshot).
// Used as the merge base when a branch was created without a main base snapshot.
func (s *Store) GetOldestSnapshot(branchID string) (*Snapshot, error) {
	row := s.db.QueryRow(
		`SELECT `+snapshotSelectCols+
			` FROM snapshots WHERE branch_id = ? ORDER BY timestamp ASC, rowid ASC LIMIT 1`,
		branchID,
	)
	snap, err := scanSnapshot(row)
	if err != nil {
		return nil, fmt.Errorf("no snapshots on this branch")
	}
	return snap, nil
}

// DeleteSnapshot removes a snapshot and its associated file records.
func (s *Store) DeleteSnapshot(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE snapshot_id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM snapshots WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ProtectedSnapshotIDs returns snapshot IDs that must not be deleted:
//   - base_snapshot_id of any branch with status 'active'
//   - any snapshot carrying at least one tag
//   - the base/main/head snapshot of the most recent merge per branch
//
// Retention and `avc delete` both consult this so pruning can never corrupt
// an active branch's merge base, silently untag a deliberately marked
// milestone, or invalidate the record of the last merge.
func (s *Store) ProtectedSnapshotIDs(projectID string) (map[string]bool, error) {
	protected := make(map[string]bool)

	branchRows, err := s.db.Query(
		`SELECT base_snapshot_id FROM branches
		 WHERE project_id = ? AND COALESCE(status, 'active') = 'active'
		   AND base_snapshot_id IS NOT NULL AND base_snapshot_id != ''`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for branchRows.Next() {
		var id string
		if err := branchRows.Scan(&id); err != nil {
			branchRows.Close()
			return nil, err
		}
		protected[id] = true
	}
	if err := branchRows.Err(); err != nil {
		branchRows.Close()
		return nil, err
	}
	branchRows.Close()

	tagRows, err := s.db.Query(
		`SELECT DISTINCT st.snapshot_id FROM snapshot_tags st
		 JOIN snapshots s ON s.id = st.snapshot_id
		 WHERE s.project_id = ?`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for tagRows.Next() {
		var id string
		if err := tagRows.Scan(&id); err != nil {
			tagRows.Close()
			return nil, err
		}
		protected[id] = true
	}
	if err := tagRows.Err(); err != nil {
		tagRows.Close()
		return nil, err
	}
	tagRows.Close()

	mergeRows, err := s.db.Query(
		`SELECT base_snapshot_id, main_snapshot_id, head_snapshot_id
		 FROM merges m
		 WHERE project_id = ? AND started_at = (
		     SELECT MAX(started_at) FROM merges WHERE branch_id = m.branch_id
		 )`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for mergeRows.Next() {
		var base, main, head string
		if err := mergeRows.Scan(&base, &main, &head); err != nil {
			mergeRows.Close()
			return nil, err
		}
		protected[base] = true
		protected[main] = true
		protected[head] = true
	}
	if err := mergeRows.Err(); err != nil {
		mergeRows.Close()
		return nil, err
	}
	mergeRows.Close()

	return protected, nil
}

// IsSnapshotProtected reports whether snapshotID is protected from deletion
// within projectID. See ProtectedSnapshotIDs.
func (s *Store) IsSnapshotProtected(projectID, snapshotID string) (bool, error) {
	protected, err := s.ProtectedSnapshotIDs(projectID)
	if err != nil {
		return false, err
	}
	return protected[snapshotID], nil
}

// FileVersion is a single (snapshot, hash, timestamp) tuple for one file path.
// Used by annotate to load all versions in one query instead of N queries.
type FileVersion struct {
	SnapshotID string
	FileHash   string
	Timestamp  int64
}

// GetFileVersions returns all versions of relPath across all snapshots,
// ordered oldest-first. Each row is a (snapshot_id, file_hash, timestamp)
// triple. This replaces the N-query inner loop in the annotate package.
func (s *Store) GetFileVersions(relPath string) ([]FileVersion, error) {
	rows, err := s.db.Query(
		`SELECT f.snapshot_id, f.file_hash, s.timestamp
		 FROM files f
		 JOIN snapshots s ON f.snapshot_id = s.id
		 WHERE f.relative_path = ?
		 ORDER BY s.timestamp ASC`,
		relPath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []FileVersion
	for rows.Next() {
		var v FileVersion
		if err := rows.Scan(&v.SnapshotID, &v.FileHash, &v.Timestamp); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// ─── File ────────────────────────────────────────────────────────────────────

// GetSnapshotFiles returns all file records for a snapshot.
func (s *Store) GetSnapshotFiles(snapshotID string) ([]*File, error) {
	rows, err := s.db.Query(
		`SELECT id, snapshot_id, relative_path, file_hash, file_size, file_mode
		 FROM files WHERE snapshot_id = ? ORDER BY relative_path`, snapshotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		f := &File{}
		if err := rows.Scan(&f.ID, &f.SnapshotID, &f.RelativePath, &f.FileHash, &f.FileSize, &f.FileMode); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ─── Diff cache ───────────────────────────────────────────────────────────────

// UpsertDiffCache stores a computed diff result for later retrieval.
func (s *Store) UpsertDiffCache(d *DiffCache) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO diffs
		 (id, from_snapshot_id, to_snapshot_id, file_path, diff_type, old_hash, new_hash, change_summary, computed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.FromSnapshotID, d.ToSnapshotID, d.FilePath,
		d.DiffType, d.OldHash, d.NewHash, d.ChangeSummary, nowUnix(),
	)
	return err
}

// GetDiffCache retrieves cached diff rows between two snapshots.
func (s *Store) GetDiffCache(fromID, toID string) ([]*DiffCache, error) {
	rows, err := s.db.Query(
		`SELECT id, from_snapshot_id, to_snapshot_id, file_path, diff_type, old_hash, new_hash, change_summary
		 FROM diffs WHERE from_snapshot_id = ? AND to_snapshot_id = ?`, fromID, toID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var diffs []*DiffCache
	for rows.Next() {
		d := &DiffCache{}
		if err := rows.Scan(&d.ID, &d.FromSnapshotID, &d.ToSnapshotID, &d.FilePath,
			&d.DiffType, &d.OldHash, &d.NewHash, &d.ChangeSummary); err != nil {
			return nil, err
		}
		diffs = append(diffs, d)
	}
	return diffs, rows.Err()
}

// ─── Merge ────────────────────────────────────────────────────────────────────

// InsertMerge persists a new merge record with status "in_progress".
func (s *Store) InsertMerge(m *Merge) error {
	_, err := s.db.Exec(
		`INSERT INTO merges
		 (id, project_id, branch_id, base_snapshot_id, main_snapshot_id, head_snapshot_id, status, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		m.ID, m.ProjectID, m.BranchID, m.BaseSnapshotID, m.MainSnapshotID, m.HeadSnapshotID, m.Status, m.StartedAt,
	)
	return err
}

// GetLastMerge returns the most recent merge for a branch (any status).
func (s *Store) GetLastMerge(branchID string) (*Merge, error) {
	row := s.db.QueryRow(
		`SELECT id, project_id, branch_id, base_snapshot_id, main_snapshot_id, head_snapshot_id,
		        status, started_at, finished_at
		 FROM merges WHERE branch_id = ? ORDER BY started_at DESC LIMIT 1`,
		branchID,
	)
	m := &Merge{}
	err := row.Scan(&m.ID, &m.ProjectID, &m.BranchID, &m.BaseSnapshotID,
		&m.MainSnapshotID, &m.HeadSnapshotID, &m.Status, &m.StartedAt, &m.FinishedAt)
	if err != nil {
		return nil, fmt.Errorf("no merge record for branch")
	}
	return m, nil
}

// GetLastMergeForProject returns the most recent merge for a project,
// regardless of which branch it targeted. Merges are recorded under the
// *agent* branch's ID (see InsertMerge), so callers that only know the
// project — like Abort, which must find an in-progress merge without
// knowing which agent branch it belongs to — should use this instead of
// GetLastMerge(mainBranch.ID), which never matches.
func (s *Store) GetLastMergeForProject(projectID string) (*Merge, error) {
	row := s.db.QueryRow(
		`SELECT id, project_id, branch_id, base_snapshot_id, main_snapshot_id, head_snapshot_id,
		        status, started_at, finished_at
		 FROM merges WHERE project_id = ? ORDER BY started_at DESC, rowid DESC LIMIT 1`,
		projectID,
	)
	m := &Merge{}
	err := row.Scan(&m.ID, &m.ProjectID, &m.BranchID, &m.BaseSnapshotID,
		&m.MainSnapshotID, &m.HeadSnapshotID, &m.Status, &m.StartedAt, &m.FinishedAt)
	if err != nil {
		return nil, fmt.Errorf("no merge record for project")
	}
	return m, nil
}

// UpdateMergeStatus updates the status and finished_at timestamp of a merge.
func (s *Store) UpdateMergeStatus(id, status string, finishedAt int64) error {
	_, err := s.db.Exec(
		`UPDATE merges SET status = ?, finished_at = ? WHERE id = ?`,
		status, finishedAt, id,
	)
	return err
}

// InsertMergeFiles persists all per-file decisions for a merge in one transaction.
func (s *Store) InsertMergeFiles(files []*MergeFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO merge_files (id, merge_id, relative_path, decision, base_hash, main_hash, branch_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, f := range files {
		if _, err := stmt.Exec(f.ID, f.MergeID, f.RelativePath, f.Decision, f.BaseHash, f.MainHash, f.BranchHash); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// GetMergeFileByPath returns the per-file record for a specific path in a merge.
func (s *Store) GetMergeFileByPath(mergeID, relPath string) (*MergeFile, error) {
	row := s.db.QueryRow(
		`SELECT id, merge_id, relative_path, decision, base_hash, main_hash, branch_hash
		 FROM merge_files WHERE merge_id = ? AND relative_path = ?`,
		mergeID, relPath,
	)
	f := &MergeFile{}
	if err := row.Scan(&f.ID, &f.MergeID, &f.RelativePath, &f.Decision,
		&f.BaseHash, &f.MainHash, &f.BranchHash); err != nil {
		return nil, fmt.Errorf("file '%s' not found in merge record", relPath)
	}
	return f, nil
}

// GetMergeFiles returns all per-file records for a merge.
func (s *Store) GetMergeFiles(mergeID string) ([]*MergeFile, error) {
	rows, err := s.db.Query(
		`SELECT id, merge_id, relative_path, decision, base_hash, main_hash, branch_hash
		 FROM merge_files WHERE merge_id = ? ORDER BY relative_path`,
		mergeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*MergeFile
	for rows.Next() {
		f := &MergeFile{}
		if err := rows.Scan(&f.ID, &f.MergeID, &f.RelativePath, &f.Decision,
			&f.BaseHash, &f.MainHash, &f.BranchHash); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
