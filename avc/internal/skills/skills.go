// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package skills writes MCP server configs and agent instruction files
// for the frameworks supported by `avc init --skills <framework>`.
package skills

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/trevarix/agentic-vc/avc/internal/config"
)

// Supported framework identifiers.
const (
	FrameworkClaudeCode    = "claude-code"
	FrameworkClaudeDesktop = "claude-desktop"
	FrameworkCursor        = "cursor"
	FrameworkWindsurf      = "windsurf"
	FrameworkCodex         = "codex"
	FrameworkCopilot       = "copilot"
	FrameworkGeneric       = "generic"
)

// SupportedFrameworks lists all valid --skills values.
var SupportedFrameworks = []string{
	FrameworkClaudeCode,
	FrameworkClaudeDesktop,
	FrameworkCursor,
	FrameworkWindsurf,
	FrameworkCodex,
	FrameworkCopilot,
	FrameworkGeneric,
}

// FileAction records what happened to a single file.
type FileAction struct {
	Path   string // relative to projectRoot, or absolute for a global config
	Status string // "created" | "updated" | "removed" | "skipped"
	Reason string // non-empty when Status == "skipped"
}

// WriteResult records actions and warnings for one framework.
type WriteResult struct {
	Framework string
	Actions   []FileAction
	Warnings  []string
	NextSteps []string // what the user must do in the tool for AVC to load
}

// Options adjusts how Write sets up a framework.
type Options struct {
	// Local keeps the generated agent files out of version control by adding
	// them to .gitignore. By default they are left trackable, so a teammate
	// who clones the repository gets the same agent setup.
	Local bool
	// Hooks also installs the framework's pre-edit hook, which checkpoints the
	// project before an agent session's first edit.
	Hooks bool
}

// Validate checks every framework name before anything is written, so a typo
// in `--skills` fails before `avc init` creates a project or touches a file.
func Validate(frameworks []string) error {
	for _, f := range frameworks {
		if !isSupported(f) {
			return fmt.Errorf("unknown framework '%s'; supported: %s",
				f, strings.Join(SupportedFrameworks, ", "))
		}
	}
	return nil
}

func isSupported(framework string) bool {
	for _, s := range SupportedFrameworks {
		if s == framework {
			return true
		}
	}
	return false
}

// Write installs MCP config and agent instructions for the given framework
// with default options.
func Write(projectRoot, framework string) (*WriteResult, error) {
	return WriteWithOptions(projectRoot, framework, Options{})
}

// writers maps each framework to the function that sets it up.
var writers = map[string]func(projectRoot string, r *WriteResult) error{
	FrameworkClaudeCode:    writeClaudeCode,
	FrameworkClaudeDesktop: writeClaudeDesktop,
	FrameworkCursor:        writeCursor,
	FrameworkWindsurf:      writeWindsurf,
	FrameworkCodex:         writeCodex,
	FrameworkCopilot:       writeCopilot,
	FrameworkGeneric:       writeGeneric,
}

// WriteWithOptions installs MCP config and agent instructions for the given
// framework. MCP configs are project-level where the framework supports it;
// claude-desktop and windsurf only have global configs.
func WriteWithOptions(projectRoot, framework string, opts Options) (*WriteResult, error) {
	if err := Validate([]string{framework}); err != nil {
		return nil, err
	}
	r := &WriteResult{Framework: framework}
	if err := writers[framework](projectRoot, r); err != nil {
		return nil, err
	}
	if opts.Hooks {
		if err := writeHook(projectRoot, framework, r); err != nil {
			return nil, err
		}
	}
	if opts.Local {
		if err := gitignoreAgentFiles(projectRoot, r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// gitignoreAgentFiles adds the project files AVC wrote to .gitignore, for
// Options.Local. When the user authored the project's .gitignore, only files
// AVC created are added: a pre-existing file the user tracks (and AVC merely
// appended to or merged into) keeps its tracked status. When the .gitignore is
// AVC's own (or absent), every AVC-touched project file is added.
func gitignoreAgentFiles(projectRoot string, r *WriteResult) error {
	createdOnly := config.UserOwnsGitignore(projectRoot)
	entries := gitignoreEntriesFor(r.Actions, createdOnly)
	if len(entries) == 0 {
		return nil
	}
	action, err := config.AppendToGitignore(projectRoot, entries)
	if err != nil {
		return err
	}
	if action != "" {
		r.Actions = append(r.Actions, FileAction{Path: ".gitignore", Status: action})
	}
	return nil
}

// skillsDirPattern gitignores every generated skill directory with one entry.
const skillsDirPattern = ".claude/skills/avc-*/"

// gitignoreEntriesFor maps project files AVC wrote to .gitignore entries.
// With createdOnly, only status "created" qualifies — pre-existing files the
// user already tracks must not become ignored because AVC appended to them.
// Global config paths (absolute, outside the project) and removed files never
// produce entries.
func gitignoreEntriesFor(actions []FileAction, createdOnly bool) []string {
	var entries []string
	seen := map[string]bool{}
	for _, a := range actions {
		if a.Status == actionRemoved || (createdOnly && a.Status != actionCreated) || filepath.IsAbs(a.Path) {
			continue
		}
		entry := a.Path
		if strings.HasPrefix(entry, ".claude/skills/") {
			entry = skillsDirPattern
		}
		if seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, entry)
	}
	return entries
}

// ─── Framework writers ────────────────────────────────────────────────────────

// Project-relative paths of the files AVC writes.
const (
	projectMCPFile      = ".mcp.json"
	claudeMDFile        = "CLAUDE.md"
	agentsMDFile        = "AGENTS.md"
	legacyGenericFile   = "AGENT_INSTRUCTIONS.md"
	legacyWindsurfRules = ".windsurfrules"
)

var (
	cursorMCPFile      = filepath.Join(".cursor", "mcp.json")
	cursorRulesFile    = filepath.Join(".cursor", "rules", "avc.mdc")
	windsurfRulesFile  = filepath.Join(".windsurf", "rules", "avc.md")
	codexConfigFile    = filepath.Join(".codex", "config.toml")
	vscodeMCPFile      = filepath.Join(".vscode", "mcp.json")
	devinMCPFile       = filepath.Join(".devin", "mcp_config.json")
	copilotRulesFile   = filepath.Join(".github", "instructions", "avc.instructions.md")
	agentsMDServerNote = "configured in your agent's MCP settings (`avc init --skills <tool>` writes them)"
)

// Where each framework's MCP server is configured, as the rules intro says it.
const (
	claudeCodeLocation = "configured in `.mcp.json` and starts automatically"
	cursorLocation     = "configured in `.cursor/mcp.json` and starts automatically"
	windsurfLocation   = "configured in `.devin/mcp_config.json`"
	copilotLocation    = "configured in `.mcp.json` (and `.vscode/mcp.json` for VS Code's older Local mode)"
)

// Rule-file frontmatter. Each tool reads its own header to decide when a rule
// applies; without one, Cursor treats a rule as manual-only and the agent
// never sees it.
const (
	cursorFrontmatter   = "description: AVC version-control rules — snapshot and branch before editing\nalwaysApply: true\n"
	windsurfFrontmatter = "trigger: always_on\n"
	copilotFrontmatter  = "applyTo: \"**\"\ndescription: AVC version-control rules — snapshot and branch before editing\n"
)

// legacyRulesPrefix opens the whole-file rules older AVC versions wrote
// without a managed-file marker (Cursor's avc.mdc).
const legacyRulesPrefix = "# " + rulesTitle + "\n\nAVC is active on this project."

func isLegacyRulesFile(content string) bool {
	return strings.HasPrefix(content, legacyRulesPrefix)
}

func writeClaudeCode(projectRoot string, r *WriteResult) error {
	action, err := mergeJSONServer(projectServer(projectRoot, projectMCPFile, r))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(projectMCPFile, action))
	if globalSettings, err := claudeGlobalSettingsPath(); err == nil {
		warnStaleGlobalEntry(globalSettings, "avc", r)
	}
	if err := writeClaudeRules(projectRoot, claudeCodeLocation, configuredTier(FrameworkClaudeCode, filepath.Join(projectRoot, projectMCPFile)), r); err != nil {
		return err
	}
	r.NextSteps = append(r.NextSteps, "Restart Claude Code (or run /mcp) so it loads the avc server from .mcp.json.")
	return nil
}

func writeClaudeDesktop(projectRoot string, r *WriteResult) error {
	// Use "avc:<project-name>" as the server key so multiple projects can
	// coexist in the config without overwriting each other.
	desktopConfig, err := claudeDesktopConfigPath()
	if err != nil {
		return err
	}
	command := resolveGlobalCommand(r)
	env := map[string]any{"AVC_PROJECT": projectRoot}
	action, err := mergeJSONServer(jsonServer{
		path:       desktopConfig,
		serversKey: keyMCPServers,
		name:       "avc:" + filepath.Base(projectRoot),
		entry:      map[string]any{"command": command, "args": serverArgs(), "env": env},
		matches:    commandMatches(command, env),
	})
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(desktopConfig, action))
	if err := writeClaudeRules(projectRoot, "configured in Claude Desktop", defaultToolTier, r); err != nil {
		return err
	}
	r.NextSteps = append(r.NextSteps, "Restart Claude Desktop to load the avc server.")
	return nil
}

// writeClaudeRules writes the CLAUDE.md section and the bundled skill files.
func writeClaudeRules(projectRoot, serverLocation, tier string, r *WriteResult) error {
	action, err := upsertBlock(filepath.Join(projectRoot, claudeMDFile), renderBlock(serverLocation, tier))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(claudeMDFile, action))
	return writeClaudeSkillFiles(projectRoot, r)
}

func writeCursor(projectRoot string, r *WriteResult) error {
	action, err := mergeJSONServer(projectServer(projectRoot, cursorMCPFile, r))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(cursorMCPFile), action))
	if globalMCP, err := cursorGlobalMCPPath(); err == nil {
		warnStaleGlobalEntry(globalMCP, "avc", r)
	}

	content := renderManagedFile(cursorFrontmatter, cursorLocation, configuredTier(FrameworkCursor, filepath.Join(projectRoot, cursorMCPFile)))
	action, err = writeManagedFile(filepath.Join(projectRoot, cursorRulesFile), content, isLegacyRulesFile)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(cursorRulesFile), action))
	r.NextSteps = append(r.NextSteps, "Open Cursor Settings → MCP and enable the avc server.")
	return nil
}

// writeWindsurf sets up Windsurf, which became Devin Desktop.
//
// Devin Desktop reads a project-level .devin/mcp_config.json (mcpServers),
// shared through version control, so the server is scoped to this project.
// Windsurf before the rename had only the global
// ~/.codeium/windsurf/mcp_config.json; that entry is written only when such
// an install exists. Rules go to .windsurf/rules/avc.md, which both read.
func writeWindsurf(projectRoot string, r *WriteResult) error {
	action, err := mergeJSONServer(projectServer(projectRoot, devinMCPFile, r))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(devinMCPFile), action))

	if err := writeLegacyWindsurfServer(projectRoot, r); err != nil {
		return err
	}

	content := renderManagedFile(windsurfFrontmatter, windsurfLocation, configuredTier(FrameworkWindsurf, filepath.Join(projectRoot, devinMCPFile)))
	action, err = writeManagedFile(filepath.Join(projectRoot, windsurfRulesFile), content, nil)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(windsurfRulesFile), action))

	// Older versions wrote the rules into the legacy .windsurfrules file. Two
	// copies would drift, so the old AVC section is removed.
	removed, err := removeBlock(filepath.Join(projectRoot, legacyWindsurfRules))
	if err != nil {
		return err
	}
	if removed != "" {
		r.Actions = append(r.Actions, fileAction(legacyWindsurfRules, removed))
		r.Warnings = append(r.Warnings, "moved the AVC rules from the legacy .windsurfrules to "+filepath.ToSlash(windsurfRulesFile))
	}
	r.NextSteps = append(r.NextSteps, "In Devin Desktop (formerly Windsurf), open the MCP servers panel and refresh so it loads avc from .devin/mcp_config.json.")
	return nil
}

// writeLegacyWindsurfServer adds this project to the avc entry in a
// pre-rename Windsurf's global MCP config, when that install exists. That
// config is shared by every project, so each project is appended as a search
// root rather than replacing the last; the server also selects the open
// project from MCP roots.
func writeLegacyWindsurfServer(projectRoot string, r *WriteResult) error {
	globalMCP, err := windsurfGlobalMCPPath()
	if err != nil {
		return err
	}
	if !fileExists(filepath.Dir(globalMCP)) {
		return nil // no pre-rename Windsurf on this machine
	}
	root, _, err := readJSONObject(globalMCP)
	if err != nil {
		return err
	}
	servers, _ := root[keyMCPServers].(map[string]any)
	existing, _ := servers["avc"].(map[string]any)

	command := resolveGlobalCommand(r)
	action, err := mergeJSONServer(jsonServer{
		path:       globalMCP,
		serversKey: keyMCPServers,
		name:       "avc",
		entry:      map[string]any{"command": command, "args": serverArgs(windsurfRoots(existing, projectRoot)...)},
		matches: func(e map[string]any) bool {
			return e["command"] == command && argsContain(e, projectRoot)
		},
	})
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(globalMCP, action))
	if action != skipAlreadyConfigured {
		r.Warnings = append(r.Warnings, "also registered avc in the pre-rename Windsurf global config; Devin Desktop uses .devin/mcp_config.json, so remove the global entry once you have moved to Devin Desktop")
	}
	return nil
}

func writeCodex(projectRoot string, r *WriteResult) error {
	action, err := mergeCodexServer(filepath.Join(projectRoot, codexConfigFile), resolveProjectCommand(r))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(codexConfigFile), action))
	if err := writeAgentsMD(projectRoot, configuredTier(FrameworkCodex, filepath.Join(projectRoot, codexConfigFile)), r); err != nil {
		return err
	}
	r.NextSteps = append(r.NextSteps,
		"Codex reads .codex/config.toml only for trusted projects — trust this project in Codex, or register the server globally with: codex mcp add avc -- avc mcp serve --tools "+defaultToolTier)
	return nil
}

// copilotCodingAgentConfig is the JSON a user pastes into the repository's
// Settings → Copilot → MCP servers box for the Copilot coding agent, which
// reads no MCP file from the repository. "tools" is required there.
const copilotCodingAgentConfig = `{"mcpServers": {"avc": {"type": "local", "command": "avc", "args": ["mcp", "serve", "--tools", "` + defaultToolTier + `"], "tools": ["*"]}}}`

// writeCopilot sets up GitHub Copilot in VS Code.
//
// VS Code's Copilot agent reads the portable .mcp.json (mcpServers) at the
// workspace root — the same file Claude Code reads. The older .vscode/mcp.json
// (servers) belongs to VS Code's Local agent mode, deprecated for removal in
// February 2027, and is kept until then so both modes find the server.
func writeCopilot(projectRoot string, r *WriteResult) error {
	command := resolveProjectCommand(r)
	stdio := map[string]any{"type": "stdio", "command": command, "args": serverArgs()}

	for _, target := range []struct{ rel, key string }{
		{projectMCPFile, keyMCPServers},
		{vscodeMCPFile, keyVSCode},
	} {
		action, err := mergeJSONServer(jsonServer{
			path:       filepath.Join(projectRoot, target.rel),
			serversKey: target.key,
			name:       "avc",
			entry:      stdio,
			matches:    commandMatches(command, nil),
		})
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			// VS Code allows comments in its config files; rewriting such a
			// file would strip them, so ask the user to add the entry instead.
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"%s is not plain JSON (it may contain comments), so it was left untouched — add an \"avc\" entry under %q with command %q and args %q",
				filepath.ToSlash(target.rel), target.key, command, strings.Join(serverArgs(), " ")))
			action, err = "skipped:not plain JSON", nil
		}
		if err != nil {
			return err
		}
		r.Actions = append(r.Actions, fileAction(filepath.ToSlash(target.rel), action))
	}

	content := renderManagedFile(copilotFrontmatter, copilotLocation, configuredTier(FrameworkCopilot, filepath.Join(projectRoot, projectMCPFile)))
	action, err := writeManagedFile(filepath.Join(projectRoot, copilotRulesFile), content, nil)
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(filepath.ToSlash(copilotRulesFile), action))
	r.NextSteps = append(r.NextSteps,
		"VS Code's Copilot agent reads .mcp.json at the workspace root while the chat.mcp.workspaceRootConfig.enabled setting is on (experimental); its older Local mode reads .vscode/mcp.json. Start the avc server from the MCP servers list.",
		"For the Copilot coding agent on GitHub, paste this into the repository's Settings → Copilot → MCP servers, and install avc in .github/workflows/copilot-setup-steps.yml: "+copilotCodingAgentConfig)
	return nil
}

func writeGeneric(projectRoot string, r *WriteResult) error {
	if err := writeAgentsMD(projectRoot, defaultToolTier, r); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(projectRoot, legacyGenericFile)); err == nil {
		r.Warnings = append(r.Warnings, legacyGenericFile+" is no longer written — the AVC rules now live in AGENTS.md, which most agents read automatically; you can delete it")
	}
	r.NextSteps = append(r.NextSteps, "Point your agent's MCP settings at: avc mcp serve --tools "+defaultToolTier+" (run from this project's root).")
	return nil
}

// writeAgentsMD writes the AVC section into AGENTS.md, the instruction file
// read by Codex, Copilot, Cursor, Windsurf and other agents.
func writeAgentsMD(projectRoot, tier string, r *WriteResult) error {
	action, err := upsertBlock(filepath.Join(projectRoot, agentsMDFile), renderBlock(agentsMDServerNote, tier))
	if err != nil {
		return err
	}
	r.Actions = append(r.Actions, fileAction(agentsMDFile, action))
	return nil
}

// configuredTier returns the tool tier in the framework's MCP config after it
// was written, so the rules describe the tools the server actually exposes —
// including a tier the user changed by hand.
func configuredTier(framework, mcpPath string) string {
	return tierOf(readServerEntry(framework, mcpPath))
}

// projectServer describes the standard project-level "avc" entry in an
// mcpServers-style config.
func projectServer(projectRoot, rel string, r *WriteResult) jsonServer {
	command := resolveProjectCommand(r)
	return jsonServer{
		path:       filepath.Join(projectRoot, rel),
		serversKey: keyMCPServers,
		name:       "avc",
		entry:      map[string]any{"command": command, "args": serverArgs()},
		matches:    commandMatches(command, nil),
	}
}

// fileAction constructs a FileAction from a relative path and raw action
// string: "created", "updated", "removed", or "skipped:<reason>".
func fileAction(rel, action string) FileAction {
	parts := strings.SplitN(action, ":", 2)
	fa := FileAction{Path: rel, Status: parts[0]}
	if len(parts) == 2 {
		fa.Reason = parts[1]
	}
	return fa
}

// ─── Global config paths ──────────────────────────────────────────────────────

// claudeGlobalSettingsPath returns the path to ~/.claude.json.
// This is the global Claude Code config file read by all interfaces (VSCode
// extension, desktop app, CLI) regardless of the current project or CWD.
func claudeGlobalSettingsPath() (string, error) {
	return homePath(".claude.json")
}

// claudeDesktopConfigPath returns the OS-specific path to the Claude Desktop
// MCP config file.
//
//	macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json
//	Windows: %APPDATA%\Claude\claude_desktop_config.json
//	Linux:   ~/.config/Claude/claude_desktop_config.json
func claudeDesktopConfigPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return homePath("Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		return filepath.Join(appdata, "Claude", "claude_desktop_config.json"), nil
	default:
		return homePath(".config", "Claude", "claude_desktop_config.json")
	}
}

// cursorGlobalMCPPath returns the path to ~/.cursor/mcp.json.
func cursorGlobalMCPPath() (string, error) {
	return homePath(".cursor", "mcp.json")
}

// windsurfGlobalMCPPath returns the path to ~/.codeium/windsurf/mcp_config.json,
// the one MCP config every Windsurf window loads.
func windsurfGlobalMCPPath() (string, error) {
	return homePath(".codeium", "windsurf", "mcp_config.json")
}

// homePath joins elems under the user's home directory.
func homePath(elems ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(append([]string{home}, elems...)...), nil
}

// ─── MCP command resolution ───────────────────────────────────────────────────

// resolveBinaryPath returns the absolute path to the running avc binary.
// EvalSymlinks is applied so Homebrew and similar symlink installations resolve
// to the real binary path that the OS can execute without a shell PATH lookup.
// isTmp reports a binary in a temp directory (i.e. run via `go run`), whose
// path won't be stable.
func resolveBinaryPath() (path string, isTmp bool) {
	exe, err := os.Executable()
	if err != nil {
		return "avc", false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return resolved, strings.HasPrefix(resolved, os.TempDir())
}

// resolveGlobalCommand returns the command for a global (machine-local) MCP
// config: the absolute path of the running binary, so the framework can start
// the server without a shell PATH lookup.
func resolveGlobalCommand(r *WriteResult) string {
	path, isTmp := resolveBinaryPath()
	if isTmp {
		r.Warnings = append(r.Warnings, tmpBinaryWarning)
	}
	return path
}

// resolveProjectCommand returns the command for a project-level (committable,
// shared) MCP config. Prefers the bare "avc" name so the config works on any
// machine with avc on PATH; falls back to the absolute binary path with a
// warning when avc is not on PATH.
func resolveProjectCommand(r *WriteResult) string {
	if _, err := exec.LookPath("avc"); err == nil {
		return "avc"
	}
	path, isTmp := resolveBinaryPath()
	if isTmp {
		r.Warnings = append(r.Warnings, tmpBinaryWarning)
	} else {
		r.Warnings = append(r.Warnings,
			"avc is not on your PATH — wrote an absolute binary path to the project MCP config; teammates will need to adjust it. Install avc on PATH and re-run `avc init --skills` to make the config portable.")
	}
	return path
}

const tmpBinaryWarning = "avc binary appears to be a temporary build (go run) — the MCP server path in the config may not be stable. Install avc to a permanent location and re-run `avc init --skills`."

// ─── Claude Code skill files ──────────────────────────────────────────────────

// skillAssets holds the Claude skill files bundled into the binary. The AVC
// Claude plugin publishes this same directory, so the skills installed by
// `avc init --skills` and the skills shipped to plugin users cannot drift.
//
//go:embed assets/skills
var skillAssets embed.FS

// skillAssetsRoot is the embedded directory holding one <name>/SKILL.md per skill.
const skillAssetsRoot = "assets/skills"

// bundledSkill is one skill file bundled into the binary.
type bundledSkill struct {
	Path    string // project-relative destination, e.g. .claude/skills/avc-run/SKILL.md
	Content string
}

// claudeSkillFiles returns every bundled Claude skill. Order is deterministic:
// embed.FS.ReadDir returns entries sorted by filename.
func claudeSkillFiles() ([]bundledSkill, error) {
	entries, err := skillAssets.ReadDir(skillAssetsRoot)
	if err != nil {
		return nil, fmt.Errorf("read bundled skills: %w", err)
	}
	var skills []bundledSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := skillAssets.ReadFile(skillAssetsRoot + "/" + e.Name() + "/SKILL.md")
		if err != nil {
			return nil, fmt.Errorf("read bundled skill %s: %w", e.Name(), err)
		}
		skills = append(skills, bundledSkill{
			Path:    ".claude/skills/" + e.Name() + "/SKILL.md",
			Content: string(data),
		})
	}
	return skills, nil
}

// writeClaudeSkillFiles installs the bundled skill files into the project.
func writeClaudeSkillFiles(projectRoot string, r *WriteResult) error {
	skills, err := claudeSkillFiles()
	if err != nil {
		return err
	}
	for _, s := range skills {
		path := filepath.Join(projectRoot, filepath.FromSlash(s.Path))
		action, err := writeFileIfAbsent(path, s.Content)
		if err != nil {
			return err
		}
		r.Actions = append(r.Actions, fileAction(s.Path, action))
	}
	return nil
}
