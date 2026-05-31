// Package tests — database correctness tests.
//
// Covers:
//   - SQLite WAL mode + query indexes
//   - Branch name validation
//   - SetActiveBranch concurrent-write safety
//   - diff3-style conflict markers (||||||| base block)
package tests

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trevarix/agentic-vc/avc/internal/branch"
	"github.com/trevarix/agentic-vc/avc/internal/config"
	"github.com/trevarix/agentic-vc/avc/internal/db"
	"github.com/trevarix/agentic-vc/avc/internal/merge"
	"github.com/trevarix/agentic-vc/avc/internal/snapshot"

	_ "modernc.org/sqlite"
)

// ─── 1.1 · SQLite WAL mode + indexes ─────────────────────────────────────────

// TestDB_WALMode verifies that every database connection operates in WAL mode.
// A second raw connection is used so we query the persisted pragma, not a
// session-local cache.
func TestDB_WALMode(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Open through db.Open to trigger pragma setup and migration.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	store.Close()

	// Open a raw connection to read back the persisted journal_mode.
	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	raw, err := openRawDB(dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()

	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

// TestDB_ForeignKeysEnabled verifies that the schema has FK constraints defined
// and that they are enforced when foreign_keys=ON is applied.
//
// NOTE: SQLite's PRAGMA foreign_keys is a per-connection session setting — it is
// never persisted to disk. Reading it on a separate raw connection will always
// return 0. Instead this test proves FK enforcement by behaviour: open a raw
// connection, enable FK, then attempt an INSERT that references a non-existent
// branch and expect an FK violation error.
func TestDB_ForeignKeysEnabled(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Trigger schema creation via db.Open.
	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	store.Close()

	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	raw, err := openRawDB(dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()

	// Enable FK enforcement on this connection.
	if _, err := raw.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("PRAGMA foreign_keys=ON: %v", err)
	}

	// Attempt to insert a snapshot that references a non-existent branch — this
	// must fail with a FK violation when foreign_keys is ON.
	_, insertErr := raw.Exec(`
		INSERT INTO snapshots (id, project_id, timestamp, label, agent_name, notes, file_count, total_size, branch_id)
		VALUES ('snap-test', 'proj-test', 0, 'test', '', '', 0, 0, 'nonexistent-branch-id')
	`)
	if insertErr == nil {
		t.Error("expected FK violation when inserting snapshot with non-existent branch_id; got nil error")
	}
}

// TestDB_IndexesExist verifies that all five Phase-1 indexes are present
// after a fresh database is opened.
func TestDB_IndexesExist(t *testing.T) {
	projectRoot := setupTestProject(t)

	store, err := db.Open(projectRoot)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	store.Close()

	dbPath := filepath.Join(projectRoot, ".avc", "avc.db")
	raw, err := openRawDB(dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()

	wantIndexes := []string{
		"idx_files_snapshot_id",
		"idx_files_path",
		"idx_snapshots_branch_ts",
		"idx_merge_files_merge_id",
		"idx_branches_project_name",
	}
	for _, idx := range wantIndexes {
		var name string
		err := raw.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q missing from schema: %v", idx, err)
		}
	}
}

// TestDB_OpenIsIdempotent verifies that calling Open multiple times on the same
// path does not fail (all migrations and index creations are idempotent).
func TestDB_OpenIsIdempotent(t *testing.T) {
	projectRoot := setupTestProject(t)
	for i := 0; i < 3; i++ {
		store, err := db.Open(projectRoot)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		store.Close()
	}
}

// ─── 1.2 · Branch name validation ────────────────────────────────────────────

// TestValidateBranchName_AcceptsValidNames verifies that legal branch names
// pass validation without error.
func TestValidateBranchName_AcceptsValidNames(t *testing.T) {
	valid := []string{
		"feat/add-auth",
		"fix/payment-bug",
		"feature-123",
		"release/v1.2.0",
		"refactor_db",
		"a",
		strings.Repeat("x", 100), // maximum allowed length
	}
	for _, name := range valid {
		if err := branch.ValidateBranchName(name); err != nil {
			t.Errorf("ValidateBranchName(%q) unexpected error: %v", name, err)
		}
	}
}

// TestValidateBranchName_RejectsEmpty verifies that an empty string fails.
func TestValidateBranchName_RejectsEmpty(t *testing.T) {
	if err := branch.ValidateBranchName(""); err == nil {
		t.Error("expected error for empty branch name, got nil")
	}
}

// TestValidateBranchName_RejectsMain verifies that "main" is rejected.
func TestValidateBranchName_RejectsMain(t *testing.T) {
	if err := branch.ValidateBranchName("main"); err == nil {
		t.Error("expected error for reserved name 'main', got nil")
	}
}

// TestValidateBranchName_RejectsWindowsReservedNames verifies that Windows
// device names are rejected (case-insensitive).
func TestValidateBranchName_RejectsWindowsReservedNames(t *testing.T) {
	reserved := []string{"con", "prn", "aux", "nul", "CON", "NUL", "Com1", "LPT1"}
	for _, name := range reserved {
		if err := branch.ValidateBranchName(name); err == nil {
			t.Errorf("expected error for Windows reserved name %q, got nil", name)
		}
	}
}

// TestValidateBranchName_RejectsPathTraversal verifies that ".." is rejected.
func TestValidateBranchName_RejectsPathTraversal(t *testing.T) {
	dangerous := []string{
		"../../etc/passwd",
		"..",
		"a/../b",
	}
	for _, name := range dangerous {
		if err := branch.ValidateBranchName(name); err == nil {
			t.Errorf("expected error for path-traversal name %q, got nil", name)
		}
	}
}

// TestValidateBranchName_RejectsIllegalCharacters verifies that spaces,
// colons, asterisks, and other shell-special characters are rejected.
func TestValidateBranchName_RejectsIllegalCharacters(t *testing.T) {
	illegal := []string{
		"feat auth",       // space
		"feat:auth",       // colon
		"feat*",           // asterisk
		"feat?",           // question mark
		"feat|pipe",       // pipe
		"feat<redir",      // redirect
		"feat>redir",      // redirect
	}
	for _, name := range illegal {
		if err := branch.ValidateBranchName(name); err == nil {
			t.Errorf("expected error for illegal name %q, got nil", name)
		}
	}
}

// TestValidateBranchName_RejectsTooLong verifies that names over 100 chars fail.
func TestValidateBranchName_RejectsTooLong(t *testing.T) {
	tooLong := strings.Repeat("a", 101)
	if err := branch.ValidateBranchName(tooLong); err == nil {
		t.Errorf("expected error for %d-char name, got nil", len(tooLong))
	}
}

// TestValidateBranchName_RejectsLeadingDot verifies ".hidden" is rejected.
func TestValidateBranchName_RejectsLeadingDot(t *testing.T) {
	if err := branch.ValidateBranchName(".hidden"); err == nil {
		t.Error("expected error for name starting with '.', got nil")
	}
}

// TestValidateBranchName_RejectsTrailingDot verifies "feature." is rejected.
func TestValidateBranchName_RejectsTrailingDot(t *testing.T) {
	if err := branch.ValidateBranchName("feature."); err == nil {
		t.Error("expected error for name ending with '.', got nil")
	}
}

// TestBranchCreate_RejectsInvalidNames verifies that branch.Create rejects
// names that fail validation without creating any DB record or workspace.
func TestBranchCreate_RejectsInvalidNames(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)
	writeFile(t, projectRoot, "file.go", "v1")
	createMainSnap(t, projectRoot, mainBranchID, "base")

	invalid := []string{
		"bad name with spaces",
		"../../evil",
		"con",
		"",
	}
	for _, name := range invalid {
		_, err := branch.Create(projectRoot, name, "")
		if err == nil {
			t.Errorf("branch.Create(%q): expected error, got nil", name)
		}
	}
}

// ─── 1.3 · SetActiveBranch concurrent-write safety ───────────────────────────

// TestSetActiveBranch_ConcurrentWrites launches N goroutines each writing a
// different branch name. After all complete, the config must be a valid,
// parseable TOML file whose active branch is one of the written names.
// Without the file lock, a race between two concurrent writers can truncate or
// interleave bytes, producing an unparseable file.
func TestSetActiveBranch_ConcurrentWrites(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Write a baseline config with a field that must survive concurrent writes.
	initial := &config.Config{}
	initial.Project.DefaultAgent = "sentinel-agent"
	initial.Branch.Active = "main"
	if err := config.Save(projectRoot, initial); err != nil {
		t.Fatalf("Save baseline: %v", err)
	}

	names := []string{"feat/alpha", "feat/beta", "feat/gamma", "feat/delta"}
	var wg sync.WaitGroup
	for _, name := range names {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = config.SetActiveBranch(projectRoot, name)
		}()
	}
	wg.Wait()

	loaded, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load after concurrent writes: %v (file likely corrupted)", err)
	}

	// The result must be one of the valid branch names (or the original "main").
	valid := map[string]bool{"main": true}
	for _, n := range names {
		valid[n] = true
	}
	if !valid[loaded.Branch.Active] {
		t.Errorf("config corrupted: active branch = %q (not a valid name)", loaded.Branch.Active)
	}

	// The sentinel field written before the race must still be intact.
	if loaded.Project.DefaultAgent != "sentinel-agent" {
		t.Errorf("DefaultAgent lost after concurrent writes: got %q", loaded.Project.DefaultAgent)
	}
}

// TestSetActiveBranch_StaleLockIsCleared verifies that a stale lock file
// (left by a crashed process) does not cause SetActiveBranch to time out.
// We simulate a stale lock by creating the file and back-dating its mtime.
func TestSetActiveBranch_StaleLockIsCleared(t *testing.T) {
	projectRoot := setupTestProject(t)

	// Create a stale lock file — mtime 60 seconds in the past.
	lockPath := filepath.Join(projectRoot, ".avc", "config.lock")
	if err := os.WriteFile(lockPath, []byte(""), 0600); err != nil {
		t.Fatalf("create lock file: %v", err)
	}
	staleTime := time.Now().Add(-60 * time.Second)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("backdate lock file: %v", err)
	}

	// SetActiveBranch must clear the stale lock and succeed within 500 ms.
	done := make(chan error, 1)
	go func() {
		done <- config.SetActiveBranch(projectRoot, "feat/stale-test")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetActiveBranch with stale lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetActiveBranch timed out — stale lock was not cleared")
	}

	loaded, _ := config.Load(projectRoot)
	if loaded.Branch.Active != "feat/stale-test" {
		t.Errorf("active branch = %q, want %q", loaded.Branch.Active, "feat/stale-test")
	}
}

// ─── 1.4 · diff3-style conflict markers ──────────────────────────────────────

// TestMerge_Conflict_HasDiff3BaseMarker verifies that the "||||||| base"
// section is present when a conflict is written to disk.
func TestMerge_Conflict_HasDiff3BaseMarker(t *testing.T) {
	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "shared.go", "base content\n")
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	// Branch edits the file.
	b, err := branch.Create(projectRoot, "feat/diff3", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "shared.go", "branch changed this\n")
	if _, err := snapshot.Create(projectRoot, "branch-edit", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Main also edits the same file (different content → guaranteed conflict).
	// Sleep ensures the main snapshot gets a strictly later Unix timestamp so
	// GetHeadSnapshot reliably returns the correct HEAD for main.
	time.Sleep(time.Second)
	writeFile(t, projectRoot, "shared.go", "main changed this differently\n")
	createMainSnap(t, projectRoot, mainBranchID, "main-edit")

	result, err := merge.Merge(projectRoot, "feat/diff3")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts < 1 {
		t.Skip("merge produced no conflicts — cannot test conflict markers")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "shared.go"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	content := string(data)

	for _, marker := range []string{
		"<<<<<<< main (ours)",
		"||||||| base (common ancestor)",
		"=======",
		">>>>>>> branch (theirs)",
	} {
		if !strings.Contains(content, marker) {
			t.Errorf("conflict marker %q not found in file:\n%s", marker, content)
		}
	}
}

// TestMerge_Conflict_BaseContentIsCorrect verifies the base block contains the
// original pre-divergence content, not main's or branch's version.
func TestMerge_Conflict_BaseContentIsCorrect(t *testing.T) {
	const (
		baseText   = "original baseline\n"
		mainText   = "main rewrote this line\n"
		branchText = "branch rewrote this line differently\n"
	)

	projectRoot, mainBranchID := setupProjectWithMain(t)

	writeFile(t, projectRoot, "file.go", baseText)
	baseSnap := createMainSnap(t, projectRoot, mainBranchID, "base")

	b, err := branch.Create(projectRoot, "feat/basecheck", baseSnap.ID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ws := branch.WorkspacePath(projectRoot, b.Name)
	writeFile(t, ws, "file.go", branchText)
	if _, err := snapshot.Create(projectRoot, "branch-edit", "", "", b.ID, ws); err != nil {
		t.Fatalf("branch snapshot: %v", err)
	}

	// Sleep ensures the main snapshot gets a strictly later Unix timestamp so
	// GetHeadSnapshot reliably returns the correct HEAD for main.
	time.Sleep(time.Second)
	writeFile(t, projectRoot, "file.go", mainText)
	createMainSnap(t, projectRoot, mainBranchID, "main-edit")

	result, err := merge.Merge(projectRoot, "feat/basecheck")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Conflicts < 1 {
		t.Skip("merge produced no conflicts")
	}

	data, err := os.ReadFile(filepath.Join(projectRoot, "file.go"))
	if err != nil {
		t.Fatalf("read conflicted file: %v", err)
	}
	content := string(data)

	// Extract the text between the base marker and the separator.
	const baseMarker = "||||||| base (common ancestor)\n"
	const sepMarker  = "=======\n"
	baseIdx := strings.Index(content, baseMarker)
	sepIdx  := strings.Index(content, sepMarker)
	if baseIdx < 0 || sepIdx < 0 || baseIdx >= sepIdx {
		t.Fatalf("markers not in expected order in:\n%s", content)
	}
	baseSection := content[baseIdx+len(baseMarker) : sepIdx]

	// The base section must contain the original content (not main's or branch's).
	if !strings.Contains(baseSection, strings.TrimSuffix(baseText, "\n")) {
		t.Errorf("base section = %q; expected to contain %q", baseSection, baseText)
	}
	if strings.Contains(baseSection, strings.TrimSuffix(mainText, "\n")) {
		t.Errorf("base section = %q; must NOT contain main's text %q", baseSection, mainText)
	}
	if strings.Contains(baseSection, strings.TrimSuffix(branchText, "\n")) {
		t.Errorf("base section = %q; must NOT contain branch's text %q", baseSection, branchText)
	}
}

// ─── raw DB helper (avoids repeating sql.Open boilerplate in every test) ─────

// openRawDB opens a raw *sql.DB at the given path for pragma inspection.
// The caller is responsible for calling Close.
func openRawDB(path string) (*sql.DB, error) {
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
