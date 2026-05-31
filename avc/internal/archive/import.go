package archive

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/db"
	_ "modernc.org/sqlite"
)

// ImportResult summarises a completed import.
type ImportResult struct {
	BundlePath    string
	ProjectName   string
	SnapshotCount int
	ObjectCount   int
	SkippedRows   int // INSERT OR IGNORE rows that already existed
}

// Import reads a bundle produced by Export and merges its contents into the
// AVC project at projectRoot.
//
// Objects are written into .avc/objects/ using content-addressed paths — if a
// blob already exists it is silently skipped (content-addressing deduplicates
// automatically).
//
// DB rows are replayed with INSERT OR IGNORE, so snapshots/branches that
// already exist (same primary key) are left unchanged.
func Import(projectRoot, bundlePath string) (*ImportResult, error) {
	// Ensure the target is an initialised AVC project.
	store, err := db.Open(projectRoot)
	if err != nil {
		return nil, err
	}
	store.Close()

	// ── Parse bundle version ─────────────────────────────────────────────────
	manifest, err := ReadManifest(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if manifest.Version != exportFormatVersion {
		return nil, fmt.Errorf(
			"bundle version %q is not supported (expected %q); "+
				"re-export with a matching avc version",
			manifest.Version, exportFormatVersion,
		)
	}

	// ── Stream through the archive ───────────────────────────────────────────
	f, err := os.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	objectsDir := filepath.Join(projectRoot, ".avc", "objects")
	result := &ImportResult{
		BundlePath:  bundlePath,
		ProjectName: manifest.ProjectName,
	}

	var sqlDump string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}

		switch {
		case hdr.Name == "avc-export.json":
			// Already read via ReadManifest; drain and skip.
			_, _ = io.Copy(io.Discard, tr)

		case hdr.Name == "schema.sql":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read schema.sql: %w", err)
			}
			sqlDump = string(data)

		case strings.HasPrefix(hdr.Name, "objects/"):
			// Reconstruct hash: objects/<shard>/<rest>
			rel := strings.TrimPrefix(hdr.Name, "objects/")
			parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
			if len(parts) != 2 {
				_, _ = io.Copy(io.Discard, tr)
				continue
			}
			hash := parts[0] + parts[1]
			destPath := filepath.Join(objectsDir, parts[0], parts[1])

			// Skip if object already present.
			if _, statErr := os.Stat(destPath); statErr == nil {
				_, _ = io.Copy(io.Discard, tr)
				continue
			}

			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return nil, fmt.Errorf("create object dir for %s: %w", hash[:8], err)
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read object %s: %w", hash[:8], err)
			}
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return nil, fmt.Errorf("write object %s: %w", hash[:8], err)
			}
			result.ObjectCount++

		default:
			_, _ = io.Copy(io.Discard, tr)
		}
	}

	// ── Replay SQL dump ─────────────────────────────────────────────────────
	if sqlDump == "" {
		return nil, fmt.Errorf("bundle contains no schema.sql")
	}

	skipped, imported, err := replaySQL(projectRoot, sqlDump)
	if err != nil {
		return nil, fmt.Errorf("replay SQL: %w", err)
	}
	result.SnapshotCount = imported
	result.SkippedRows = skipped

	// ── Remap imported branches to destination project ───────────────────────
	// Imported snapshots point to branch IDs from the source project.
	// Match by branch name so that `avc list` on the destination shows them
	// without requiring --all.
	if err := remapImportedBranches(projectRoot); err != nil {
		return nil, fmt.Errorf("remap branches: %w", err)
	}

	return result, nil
}

// replaySQL executes INSERT OR IGNORE statements from the dump against the
// project database. Returns (skippedRows, snapshotRowsImported, error).
func replaySQL(projectRoot, dump string) (skipped, snapshots int, err error) {
	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, 0, err
	}
	defer rawDB.Close()

	// Enable FK enforcement and WAL.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=OFF", // OFF during import to avoid ordering issues
	} {
		if _, err := rawDB.Exec(pragma); err != nil {
			return 0, 0, fmt.Errorf("pragma: %w", err)
		}
	}

	tx, err := rawDB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Execute each statement from the dump.
	stmts := splitStatements(dump)
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		res, execErr := tx.Exec(stmt)
		if execErr != nil {
			// Non-fatal: log and continue (schema mismatches on future columns).
			fmt.Fprintf(os.Stderr, "[avc import] warning: %v\n", execErr)
			continue
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			skipped++
		}
		// Count snapshot inserts.
		if strings.Contains(stmt, "INSERT OR IGNORE INTO snapshots") && rows > 0 {
			snapshots++
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}

	// Re-enable FK enforcement.
	if _, err = rawDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return skipped, snapshots, err
	}

	return skipped, snapshots, nil
}

// remapImportedBranches fixes branch ownership after a SQL replay.
//
// The dump inserts source-project branch rows with a foreign project_id that
// doesn't match the destination project. Snapshots therefore point to "orphan"
// branch IDs that avc list (which scopes to the destination branch ID) can't see.
//
// This function:
//  1. Finds every orphan branch (project_id ≠ destination project).
//  2. If a destination branch with the same name already exists, remaps all
//     snapshot/merge rows to use the destination branch ID, then deletes the
//     orphan branch record.
//  3. If no matching destination branch exists, re-parents the orphan branch
//     under the destination project so it appears in branch list and avc list --all.
//  4. Removes any orphan project records that have no remaining branches.
func remapImportedBranches(projectRoot string) error {
	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer rawDB.Close()

	// Look up the destination project by its filesystem path.
	var destProjectID string
	if err := rawDB.QueryRow(
		`SELECT id FROM projects WHERE path = ?`, projectRoot,
	).Scan(&destProjectID); err != nil {
		return fmt.Errorf("find destination project: %w", err)
	}

	// Collect destination branches: name → id.
	destBranches := make(map[string]string)
	rows, err := rawDB.Query(
		`SELECT id, name FROM branches WHERE project_id = ?`, destProjectID,
	)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		destBranches[name] = id
	}
	rows.Close()

	// Collect orphan branches (imported from another project).
	type branchRow struct{ id, name, projectID string }
	var orphans []branchRow
	rows, err = rawDB.Query(
		`SELECT id, name, project_id FROM branches WHERE project_id != ?`, destProjectID,
	)
	if err != nil {
		return err
	}
	for rows.Next() {
		var b branchRow
		if err := rows.Scan(&b.id, &b.name, &b.projectID); err != nil {
			rows.Close()
			return err
		}
		orphans = append(orphans, b)
	}
	rows.Close()

	if len(orphans) == 0 {
		return nil // nothing to remap
	}

	tx, err := rawDB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	srcProjectIDs := make(map[string]bool)
	for _, orphan := range orphans {
		srcProjectIDs[orphan.projectID] = true

		destID, exists := destBranches[orphan.name]
		if !exists {
			// No same-named branch in destination — adopt it into the dest project.
			if _, err = tx.Exec(
				`UPDATE branches SET project_id = ? WHERE id = ?`, destProjectID, orphan.id,
			); err != nil {
				return err
			}
			continue
		}

		// Same-named branch exists — remap all references then remove the orphan.
		if _, err = tx.Exec(
			`UPDATE snapshots SET branch_id = ? WHERE branch_id = ?`, destID, orphan.id,
		); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`UPDATE merges SET branch_id = ? WHERE branch_id = ?`, destID, orphan.id,
		); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`DELETE FROM branches WHERE id = ?`, orphan.id,
		); err != nil {
			return err
		}
	}

	// Remove source project records that are now branchless.
	for projID := range srcProjectIDs {
		if _, err = tx.Exec(
			`DELETE FROM projects WHERE id = ? AND NOT EXISTS (SELECT 1 FROM branches WHERE project_id = ?)`,
			projID, projID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// splitStatements splits a SQL dump on ";\n" boundaries.
// Simple splitter — sufficient for the INSERT-only dumps we generate.
func splitStatements(dump string) []string {
	var out []string
	for _, s := range strings.Split(dump, ";\n") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t+";")
		}
	}
	return out
}
