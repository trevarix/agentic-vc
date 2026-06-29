package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trevarix/agentic-vc/avc/internal/config"
)

// TestConfig_Load_ReturnsDefaults_WhenFileAbsent verifies that loading from a
// directory with no config.toml returns sensible defaults without error.
func TestConfig_Load_ReturnsDefaults_WhenFileAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Branch.Active != "main" {
		t.Errorf("default active branch = %q, want %q", cfg.Branch.Active, "main")
	}
}

// TestConfig_SaveAndLoad_RoundTrip verifies that values written with Save are
// recovered intact by Load.
func TestConfig_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	original := &config.Config{}
	original.Project.DefaultAgent = "test-agent"
	original.Branch.Active = "feature/x"
	original.Run.DefaultTimeoutSeconds = 120
	original.Run.MaxTimeoutSeconds = 300
	original.Run.MaxOutputKB = 256

	if err := config.Save(dir, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Project.DefaultAgent != original.Project.DefaultAgent {
		t.Errorf("DefaultAgent = %q, want %q", loaded.Project.DefaultAgent, original.Project.DefaultAgent)
	}
	if loaded.Branch.Active != original.Branch.Active {
		t.Errorf("Active = %q, want %q", loaded.Branch.Active, original.Branch.Active)
	}
	if loaded.Run.DefaultTimeoutSeconds != original.Run.DefaultTimeoutSeconds {
		t.Errorf("DefaultTimeoutSeconds = %d, want %d",
			loaded.Run.DefaultTimeoutSeconds, original.Run.DefaultTimeoutSeconds)
	}
	if loaded.Run.MaxOutputKB != original.Run.MaxOutputKB {
		t.Errorf("MaxOutputKB = %d, want %d", loaded.Run.MaxOutputKB, original.Run.MaxOutputKB)
	}
}

// TestConfig_SetActiveBranch_UpdatesOnDisk verifies that SetActiveBranch
// changes only the active branch field and preserves other fields.
func TestConfig_SetActiveBranch_UpdatesOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	// Write initial config with custom agent.
	initial := &config.Config{}
	initial.Project.DefaultAgent = "my-agent"
	initial.Branch.Active = "main"
	if err := config.Save(dir, initial); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := config.SetActiveBranch(dir, "feat/new"); err != nil {
		t.Fatalf("SetActiveBranch: %v", err)
	}

	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Branch.Active != "feat/new" {
		t.Errorf("Active = %q, want %q", loaded.Branch.Active, "feat/new")
	}
	// Other fields must be preserved.
	if loaded.Project.DefaultAgent != "my-agent" {
		t.Errorf("DefaultAgent lost after SetActiveBranch: got %q", loaded.Project.DefaultAgent)
	}
}

// TestConfig_Load_BackfillsEmptyActiveBranch verifies that a config file with
// no active branch set gets backfilled to "main" on load.
func TestConfig_Load_BackfillsEmptyActiveBranch(t *testing.T) {
	dir := t.TempDir()
	avcDir := filepath.Join(dir, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	// Write a minimal TOML without a branch section.
	tomlContent := "[project]\ndefault_agent = \"\"\n"
	if err := os.WriteFile(filepath.Join(avcDir, "config.toml"), []byte(tomlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Branch.Active != "main" {
		t.Errorf("expected Active to be backfilled to 'main', got %q", cfg.Branch.Active)
	}
}

// TestConfig_WriteDefault_CreatesConfigAndIgnoreFiles verifies that
// WriteDefault creates config.toml and .avcignore in a fresh directory.
func TestConfig_WriteDefault_CreatesConfigAndIgnoreFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	if err := config.WriteDefault(dir); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	// config.toml must exist.
	configPath := filepath.Join(dir, ".avc", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error(".avc/config.toml was not created")
	}

	// .avcignore must exist.
	ignorePath := filepath.Join(dir, ".avcignore")
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		t.Error(".avcignore was not created")
	}
}

// TestConfig_WriteDefault_AppendsToGitignore verifies that WriteDefault adds
// .avc/ and .avcignore entries to an existing .gitignore.
func TestConfig_WriteDefault_AppendsToGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	// Create a .gitignore with existing content.
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("*.log\nnode_modules/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	if err := config.WriteDefault(dir); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, ".avc/") {
		t.Error(".avc/ not appended to .gitignore")
	}
	if !strings.Contains(content, ".avcignore") {
		t.Error(".avcignore not appended to .gitignore")
	}
	// Existing content must be preserved.
	if !strings.Contains(content, "*.log") {
		t.Error("existing .gitignore content was lost")
	}
}

// TestConfig_WriteDefault_Idempotent verifies that calling WriteDefault twice
// does not duplicate .gitignore entries.
func TestConfig_WriteDefault_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(""), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	if err := config.WriteDefault(dir); err != nil {
		t.Fatalf("WriteDefault first call: %v", err)
	}
	if err := config.WriteDefault(dir); err != nil {
		t.Fatalf("WriteDefault second call: %v", err)
	}

	data, _ := os.ReadFile(gitignorePath)
	content := string(data)

	// Count occurrences of .avc/ — must appear exactly once.
	count := strings.Count(content, ".avc/")
	if count != 1 {
		t.Errorf(".avc/ appears %d times in .gitignore, want 1", count)
	}
}

// TestConfig_WriteDefault_NoGitignore_IsNoOp verifies that WriteDefault does
// not create a .gitignore if one does not already exist.
func TestConfig_WriteDefault_NoGitignore_IsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".avc"), 0755); err != nil {
		t.Fatalf("mkdir .avc: %v", err)
	}

	if err := config.WriteDefault(dir); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); !os.IsNotExist(err) {
		t.Error("WriteDefault should not create .gitignore when none existed")
	}
}
