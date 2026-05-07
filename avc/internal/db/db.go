// Package db manages all SQLite operations for AVC.
package db

import (
	"database/sql"
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
	// Phase 4: add branch_id to snapshots (idempotent — SQLite returns an error
	// if the column already exists, which we intentionally ignore).
	_, _ = s.db.Exec(`ALTER TABLE snapshots ADD COLUMN branch_id TEXT REFERENCES branches(id)`)
	return nil
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

// InsertBranch persists a new branch record.
func (s *Store) InsertBranch(b *Branch) error {
	_, err := s.db.Exec(
		`INSERT INTO branches (id, name, project_id, base_snapshot_id, created_at)
		 VALUES (?, ?, ?, NULLIF(?, ''), ?)`,
		b.ID, b.Name, b.ProjectID, b.BaseSnapshotID, b.CreatedAt,
	)
	return err
}

// GetBranchByName returns a branch by project and name.
func (s *Store) GetBranchByName(projectID, name string) (*Branch, error) {
	row := s.db.QueryRow(
		`SELECT id, name, project_id, COALESCE(base_snapshot_id, ''), created_at
		 FROM branches WHERE project_id = ? AND name = ?`,
		projectID, name,
	)
	b := &Branch{}
	if err := row.Scan(&b.ID, &b.Name, &b.ProjectID, &b.BaseSnapshotID, &b.CreatedAt); err != nil {
		return nil, fmt.Errorf("branch '%s' not found", name)
	}
	return b, nil
}

// ListBranches returns all branches for a project ordered by creation time.
func (s *Store) ListBranches(projectID string) ([]*Branch, error) {
	rows, err := s.db.Query(
		`SELECT id, name, project_id, COALESCE(base_snapshot_id, ''), created_at
		 FROM branches WHERE project_id = ? ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []*Branch
	for rows.Next() {
		b := &Branch{}
		if err := rows.Scan(&b.ID, &b.Name, &b.ProjectID, &b.BaseSnapshotID, &b.CreatedAt); err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return branches, rows.Err()
}

// DeleteBranch removes a branch record. Snapshots on that branch are orphaned
// (their branch_id remains set) rather than deleted.
func (s *Store) DeleteBranch(id string) error {
	_, err := s.db.Exec(`DELETE FROM branches WHERE id = ?`, id)
	return err
}

// EnsureMainBranch creates the main branch for projectID if it does not exist,
// then backfills any snapshots with NULL branch_id to that branch.
// Safe to call multiple times — fully idempotent.
func (s *Store) EnsureMainBranch(projectID string) (*Branch, error) {
	row := s.db.QueryRow(
		`SELECT id, name, project_id, COALESCE(base_snapshot_id, ''), created_at
		 FROM branches WHERE project_id = ? AND name = 'main'`,
		projectID,
	)
	b := &Branch{}
	err := row.Scan(&b.ID, &b.Name, &b.ProjectID, &b.BaseSnapshotID, &b.CreatedAt)
	if err != nil {
		// Main branch absent — create it.
		b = &Branch{
			ID:        newID("branch"),
			Name:      "main",
			ProjectID: projectID,
			CreatedAt: nowUnix(),
		}
		if _, err := s.db.Exec(
			`INSERT OR IGNORE INTO branches (id, name, project_id, base_snapshot_id, created_at)
			 VALUES (?, 'main', ?, NULL, ?)`,
			b.ID, b.ProjectID, b.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("create main branch: %w", err)
		}
		// Re-fetch in case INSERT OR IGNORE hit an existing row.
		row = s.db.QueryRow(
			`SELECT id, name, project_id, COALESCE(base_snapshot_id, ''), created_at
			 FROM branches WHERE project_id = ? AND name = 'main'`,
			projectID,
		)
		if err := row.Scan(&b.ID, &b.Name, &b.ProjectID, &b.BaseSnapshotID, &b.CreatedAt); err != nil {
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

// InsertSnapshot persists a new snapshot record.
func (s *Store) InsertSnapshot(snap *Snapshot) error {
	_, err := s.db.Exec(
		`INSERT INTO snapshots
		 (id, project_id, timestamp, label, agent_name, notes, file_count, total_size, branch_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		snap.ID, snap.ProjectID, snap.Timestamp, snap.Label,
		snap.AgentName, snap.Notes, snap.FileCount, snap.TotalSize, snap.BranchID,
	)
	return err
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
			` FROM snapshots WHERE branch_id = ? ORDER BY timestamp DESC LIMIT 1`,
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
			` FROM snapshots WHERE branch_id = ? ORDER BY timestamp ASC LIMIT 1`,
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

// ─── File ────────────────────────────────────────────────────────────────────

// InsertFile persists a file record belonging to a snapshot.
func (s *Store) InsertFile(f *File) error {
	_, err := s.db.Exec(
		`INSERT INTO files (id, snapshot_id, relative_path, file_hash, file_size) VALUES (?, ?, ?, ?, ?)`,
		f.ID, f.SnapshotID, f.RelativePath, f.FileHash, f.FileSize,
	)
	return err
}

// InsertFilesBatch persists all file records in a single transaction,
// reducing SQLite fsyncs from one-per-file to one for the entire batch.
func (s *Store) InsertFilesBatch(files []*File) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO files (id, snapshot_id, relative_path, file_hash, file_size) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, f := range files {
		if _, err := stmt.Exec(f.ID, f.SnapshotID, f.RelativePath, f.FileHash, f.FileSize); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// GetSnapshotFiles returns all file records for a snapshot.
func (s *Store) GetSnapshotFiles(snapshotID string) ([]*File, error) {
	rows, err := s.db.Query(
		`SELECT id, snapshot_id, relative_path, file_hash, file_size
		 FROM files WHERE snapshot_id = ? ORDER BY relative_path`, snapshotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		f := &File{}
		if err := rows.Scan(&f.ID, &f.SnapshotID, &f.RelativePath, &f.FileHash, &f.FileSize); err != nil {
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
		 (id, from_snapshot_id, to_snapshot_id, file_path, diff_type, old_hash, new_hash, change_summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.FromSnapshotID, d.ToSnapshotID, d.FilePath,
		d.DiffType, d.OldHash, d.NewHash, d.ChangeSummary,
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
