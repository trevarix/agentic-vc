// Package db manages all SQLite operations for AVC.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

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
}

// File represents a row in the files table.
type File struct {
	ID           string
	SnapshotID   string
	RelativePath string
	FileHash     string
	FileSize     int64
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

// Open opens (or creates) the AVC SQLite database for the given project root.
func Open(projectRoot string) (*Store, error) {
	path := filepath.Join(projectRoot, dbFile)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// InitProject creates the .avc directory, initializes the schema, and inserts
// a project record. Returns the new Project.
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

	if err := store.migrate(); err != nil {
		return nil, err
	}

	project := &Project{
		ID:        newID("proj"),
		Path:      projectRoot,
		Name:      filepath.Base(projectRoot),
		CreatedAt: nowUnix(),
	}

	_, err = store.db.Exec(
		`INSERT OR IGNORE INTO projects (id, path, name, created_at) VALUES (?, ?, ?, ?)`,
		project.ID, project.Path, project.Name, project.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	return project, nil
}

// migrate creates all tables if they do not exist.
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id         TEXT PRIMARY KEY,
		path       TEXT UNIQUE,
		name       TEXT,
		created_at INTEGER
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
	`
	_, err := s.db.Exec(schema)
	return err
}

// GetProject returns the project record for the given root path.
func (s *Store) GetProject(projectRoot string) (*Project, error) {
	row := s.db.QueryRow(`SELECT id, path, name, created_at FROM projects WHERE path = ?`, projectRoot)
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Path, &p.Name, &p.CreatedAt); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	return p, nil
}

// InsertSnapshot persists a new snapshot record.
func (s *Store) InsertSnapshot(snap *Snapshot) error {
	_, err := s.db.Exec(
		`INSERT INTO snapshots (id, project_id, timestamp, label, agent_name, notes, file_count, total_size)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.ProjectID, snap.Timestamp, snap.Label,
		snap.AgentName, snap.Notes, snap.FileCount, snap.TotalSize,
	)
	return err
}

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

// ListSnapshots returns all snapshots for a project, newest first.
func (s *Store) ListSnapshots() ([]*Snapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, timestamp, label, agent_name, notes, file_count, total_size
		 FROM snapshots ORDER BY timestamp DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*Snapshot
	for rows.Next() {
		snap := &Snapshot{}
		if err := rows.Scan(&snap.ID, &snap.ProjectID, &snap.Timestamp, &snap.Label,
			&snap.AgentName, &snap.Notes, &snap.FileCount, &snap.TotalSize); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// GetSnapshot returns a single snapshot by ID.
func (s *Store) GetSnapshot(id string) (*Snapshot, error) {
	row := s.db.QueryRow(
		`SELECT id, project_id, timestamp, label, agent_name, notes, file_count, total_size
		 FROM snapshots WHERE id = ?`, id,
	)
	snap := &Snapshot{}
	if err := row.Scan(&snap.ID, &snap.ProjectID, &snap.Timestamp, &snap.Label,
		&snap.AgentName, &snap.Notes, &snap.FileCount, &snap.TotalSize); err != nil {
		return nil, fmt.Errorf("snapshot '%s' not found", id)
	}
	return snap, nil
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

// DeleteSnapshot removes a snapshot and its associated files from the database.
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
