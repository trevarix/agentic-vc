// Package archive implements avc export and avc import for cross-machine portability.
//
// Export bundle format (.avc.tar.gz):
//
//	avc-export.json          manifest: version, project name, branch list, counts
//	schema.sql               portable SQLite .dump of all rows (no binary blobs)
//	objects/<shard>/<rest>   all file blobs referenced by the exported snapshots
package archive

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	_ "modernc.org/sqlite"
)

const exportFormatVersion = "1"

// Manifest is written as avc-export.json inside every bundle.
type Manifest struct {
	Version      string   `json:"version"`
	ProjectName  string   `json:"project_name"`
	ExportedAt   int64    `json:"exported_at"`
	Branches     []string `json:"branches"`
	SnapshotCount int     `json:"snapshot_count"`
	ObjectCount  int      `json:"object_count"`
}

// ExportOptions controls what avc export includes.
type ExportOptions struct {
	// BranchName, if non-empty, limits the export to one branch's snapshots.
	// The branch record and its base snapshot are always included.
	BranchName string

	// OutputPath is the destination .tar.gz file.
	OutputPath string
}

// Export creates a portable archive of the AVC project at projectRoot.
func Export(projectRoot string, opts ExportOptions) (*Manifest, error) {
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	proj, err := store.GetProject(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project not initialized: %w", err)
	}

	// ── Decide which snapshots to export ────────────────────────────────────────
	var snapshots []*db.Snapshot
	var branchNames []string

	if opts.BranchName != "" {
		b, err := store.GetBranchByName(proj.ID, opts.BranchName)
		if err != nil {
			return nil, fmt.Errorf("branch '%s' not found: %w", opts.BranchName, err)
		}
		branchNames = []string{opts.BranchName}
		snaps, err := store.ListSnapshotsByBranch(b.ID)
		if err != nil {
			return nil, fmt.Errorf("list snapshots for branch '%s': %w", opts.BranchName, err)
		}
		snapshots = snaps
	} else {
		// All branches.
		branches, err := store.ListBranchesByStatus(proj.ID, "")
		if err != nil {
			return nil, fmt.Errorf("list branches: %w", err)
		}
		for _, b := range branches {
			branchNames = append(branchNames, b.Name)
			snaps, err := store.ListSnapshotsByBranch(b.ID)
			if err != nil {
				return nil, fmt.Errorf("list snapshots for branch '%s': %w", b.Name, err)
			}
			snapshots = append(snapshots, snaps...)
		}
	}

	// ── Collect all live object hashes ──────────────────────────────────────────
	hashes, err := collectHashes(store, snapshots)
	if err != nil {
		return nil, err
	}

	// ── Dump SQL schema ─────────────────────────────────────────────────────────
	schemaDump, err := dumpSQL(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("dump schema: %w", err)
	}

	// ── Build manifest ──────────────────────────────────────────────────────────
	manifest := &Manifest{
		Version:       exportFormatVersion,
		ProjectName:   proj.Name,
		ExportedAt:    time.Now().Unix(),
		Branches:      branchNames,
		SnapshotCount: len(snapshots),
		ObjectCount:   len(hashes),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	// ── Write .tar.gz ────────────────────────────────────────────────────────────
	out, err := os.Create(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	// manifest
	if err := writeTarBytes(tw, "avc-export.json", manifestJSON); err != nil {
		return nil, err
	}
	// schema dump
	if err := writeTarBytes(tw, "schema.sql", []byte(schemaDump)); err != nil {
		return nil, err
	}
	// object blobs
	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	for hash := range hashes {
		if len(hash) < 3 {
			continue
		}
		blobPath := filepath.Join(objectsDir, hash[:2], hash[2:])
		data, err := os.ReadFile(blobPath)
		if err != nil {
			// Object might already be GC'd; skip with a warning rather than failing.
			fmt.Fprintf(os.Stderr, "[avc export] warning: missing object %s: %v\n", hash[:8], err)
			continue
		}
		archivePath := filepath.Join("objects", hash[:2], hash[2:])
		if err := writeTarBytes(tw, archivePath, data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return manifest, gz.Close()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// collectHashes returns the set of all file hashes referenced by the given snapshots.
func collectHashes(store *db.Store, snapshots []*db.Snapshot) (map[string]bool, error) {
	hashes := make(map[string]bool)
	for _, snap := range snapshots {
		files, err := store.GetSnapshotFiles(snap.ID)
		if err != nil {
			return nil, fmt.Errorf("get files for snapshot %s: %w", snap.ID[:8], err)
		}
		for _, f := range files {
			hashes[f.FileHash] = true
		}
	}
	return hashes, nil
}

// dumpSQL produces a minimal INSERT-based SQL dump of all project tables
// using a direct SQLite query (compatible with modernc.org/sqlite without CGO).
func dumpSQL(projectRoot string) (string, error) {
	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", err
	}
	defer rawDB.Close()

	tables := []string{
		"projects", "branches", "snapshots", "files",
		"merges", "merge_files", "snapshot_tags", "project_state",
	}

	var sb strings.Builder
	sb.WriteString("-- AVC export schema dump\n")
	sb.WriteString(fmt.Sprintf("-- exported_at: %s\n\n", time.Now().Format(time.RFC3339)))

	for _, table := range tables {
		// Check the table exists.
		var n int
		err := rawDB.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&n)
		if err != nil || n == 0 {
			continue
		}

		// Get column names.
		rows, err := rawDB.Query("SELECT * FROM " + table + " LIMIT 0")
		if err != nil {
			continue
		}
		cols, err := rows.Columns()
		rows.Close()
		if err != nil || len(cols) == 0 {
			continue
		}

		// Dump rows as INSERT statements.
		dataRows, err := rawDB.Query("SELECT * FROM " + table)
		if err != nil {
			continue
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for dataRows.Next() {
			if err := dataRows.Scan(ptrs...); err != nil {
				dataRows.Close()
				return "", fmt.Errorf("scan row in %s: %w", table, err)
			}
			sb.WriteString(fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (",
				table, strings.Join(cols, ", ")))
			for i, v := range vals {
				if i > 0 {
					sb.WriteString(", ")
				}
				switch t := v.(type) {
				case nil:
					sb.WriteString("NULL")
				case int64:
					sb.WriteString(fmt.Sprintf("%d", t))
				case float64:
					sb.WriteString(fmt.Sprintf("%g", t))
				case string:
					sb.WriteString("'" + strings.ReplaceAll(t, "'", "''") + "'")
				case []byte:
					sb.WriteString("'" + strings.ReplaceAll(string(t), "'", "''") + "'")
				default:
					sb.WriteString(fmt.Sprintf("'%v'", t))
				}
			}
			sb.WriteString(");\n")
		}
		dataRows.Close()
	}

	return sb.String(), nil
}

// writeTarBytes adds a file entry to a tar archive.
func writeTarBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    filepath.ToSlash(name),
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// ReadManifest opens a bundle and returns its manifest without extracting the rest.
func ReadManifest(bundlePath string) (*Manifest, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "avc-export.json" {
			var m Manifest
			if err := json.NewDecoder(tr).Decode(&m); err != nil {
				return nil, fmt.Errorf("decode manifest: %w", err)
			}
			return &m, nil
		}
		// Skip other entries using io.Copy to drain the reader.
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("avc-export.json not found in bundle")
}

// objectsWalkDir is a helper that satisfies the fs.WalkDirFunc signature.
var _ fs.WalkDirFunc = func(path string, d fs.DirEntry, err error) error { return nil }
