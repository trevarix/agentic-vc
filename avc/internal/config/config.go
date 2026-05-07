// Package config manages the per-project AVC configuration file (.avc/config.toml).
package config

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const configFile = ".avc/config.toml"

// Config holds all runtime configuration for an AVC project.
type Config struct {
	Project ProjectConfig `toml:"project"`
	Ignore  IgnoreConfig  `toml:"ignore"`
	Branch  BranchConfig  `toml:"branch"`
	Run     RunConfig     `toml:"run"`
}

// RunConfig holds workspace command runner settings.
type RunConfig struct {
	DefaultTimeoutSeconds int `toml:"default_timeout_seconds"`
	MaxTimeoutSeconds     int `toml:"max_timeout_seconds"`
	MaxOutputKB           int `toml:"max_output_kb"`
}

// ProjectConfig holds project-level settings.
type ProjectConfig struct {
	DefaultAgent string `toml:"default_agent"`
}

// IgnoreConfig lists additional patterns to ignore beyond .avcignore.
type IgnoreConfig struct {
	ExtraPatterns []string `toml:"extra_patterns"`
}

// BranchConfig holds branch settings.
type BranchConfig struct {
	Active string `toml:"active"`
}

// defaultConfig is written during `avc init`.
var defaultConfig = Config{
	Project: ProjectConfig{DefaultAgent: ""},
	Ignore:  IgnoreConfig{ExtraPatterns: []string{}},
	Branch:  BranchConfig{Active: "main"},
}

// Load reads and parses the config file from the project root.
// Missing config files are silently ignored; defaults are returned.
func Load(projectRoot string) (*Config, error) {
	path := filepath.Join(projectRoot, configFile)
	cfg := defaultConfig

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}
	// Ensure Active is set if the file predates Phase 4.
	if cfg.Branch.Active == "" {
		cfg.Branch.Active = "main"
	}
	return &cfg, nil
}

// Save writes cfg to the config file, overwriting any existing content.
func Save(projectRoot string, cfg *Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(projectRoot, configFile), buf.Bytes(), 0644)
}

// SetActiveBranch updates only the active branch name in the config file.
func SetActiveBranch(projectRoot, name string) error {
	cfg, err := Load(projectRoot)
	if err != nil {
		c := defaultConfig
		cfg = &c
	}
	cfg.Branch.Active = name
	return Save(projectRoot, cfg)
}

// WriteDefault writes the default config.toml to the project's .avc/ directory,
// a default .avcignore to the project root, and appends .avc/ and .avcignore
// to an existing .gitignore if one is present.
func WriteDefault(projectRoot string) error {
	avcDir := filepath.Join(projectRoot, ".avc")
	if err := os.MkdirAll(avcDir, 0755); err != nil {
		return err
	}

	if err := writeFileIfAbsent(filepath.Join(projectRoot, configFile), defaultTOML); err != nil {
		return err
	}

	if err := writeFileIfAbsent(filepath.Join(projectRoot, ".avcignore"), defaultAVCIgnore); err != nil {
		return err
	}

	return appendToGitignore(projectRoot)
}

// writeFileIfAbsent writes content to path only if the file does not yet exist.
func writeFileIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// appendToGitignore adds .avc/ and .avcignore entries to an existing .gitignore.
// If no .gitignore is present, this is a no-op.
func appendToGitignore(projectRoot string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	existing := make(map[string]bool)
	for _, line := range splitLines(string(data)) {
		existing[line] = true
	}

	var toAppend []string
	for _, entry := range []string{".avc/", ".avcignore"} {
		if !existing[entry] {
			toAppend = append(toAppend, entry)
		}
	}
	if len(toAppend) == 0 {
		return nil
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	block := "\n# Agentic Version Control\n"
	for _, entry := range toAppend {
		block += entry + "\n"
	}
	_, err = f.WriteString(block)
	return err
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

const defaultTOML = `# AVC configuration file

[project]
# default_agent = "my-agent"

[ignore]
# extra_patterns = ["*.log", "dist/"]

[branch]
active = "main"

[run]
# Maximum time a workspace command can run before being killed.
default_timeout_seconds = 180
max_timeout_seconds     = 600

# Maximum output captured per stream (stdout/stderr) before truncation.
# Increase for projects with verbose test suites.
max_output_kb = 512
`

const defaultAVCIgnore = `# AVC ignore rules — patterns listed here are excluded from all snapshots.
# Syntax is identical to .gitignore.

# ── Python ────────────────────────────────────────────────────────────────────
.venv/
venv/
env/
.env/
__pycache__/
*.pyc
*.pyo
*.pyd
*.egg-info/
.pytest_cache/
.mypy_cache/
.ruff_cache/
.tox/
htmlcov/
.coverage

# ── Node / JavaScript / TypeScript ────────────────────────────────────────────
node_modules/
.npm/
.next/
.nuxt/
.svelte-kit/
*.tsbuildinfo

# ── Go ────────────────────────────────────────────────────────────────────────
vendor/

# ── Rust ──────────────────────────────────────────────────────────────────────
target/

# ── Java / Kotlin / Gradle / Maven ────────────────────────────────────────────
.gradle/
.mvn/
*.class
*.jar

# ── .NET / C# ─────────────────────────────────────────────────────────────────
obj/

# ── Ruby ──────────────────────────────────────────────────────────────────────
.bundle/

# ── Terraform ─────────────────────────────────────────────────────────────────
.terraform/
*.tfstate
*.tfstate.backup

# ── Build output (cross-stack) ────────────────────────────────────────────────
dist/
build/
out/
bin/
*.exe
*.dll
*.so
*.dylib
*.o
*.a

# ── Logs and coverage ─────────────────────────────────────────────────────────
*.log
logs/
coverage/

# ── Environment and secrets ───────────────────────────────────────────────────
.env
.env.*
.env.local
.env.production
*.pem
*.key
*.cert
*.p12
*.pfx

# ── OS artifacts ──────────────────────────────────────────────────────────────
.DS_Store
Thumbs.db
desktop.ini

# ── Editor metadata ───────────────────────────────────────────────────────────
.idea/
*.swp
*.swo
*.suo
*.user
`
