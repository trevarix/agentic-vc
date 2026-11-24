// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package skills

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Check statuses, worst last.
const (
	StatusOK   = "ok"
	StatusInfo = "info"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// Check is one finding about one framework's setup.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// AgentReport is the doctor's verdict on one framework set up in a project.
type AgentReport struct {
	Framework string  `json:"framework"`
	Checks    []Check `json:"checks"`
}

// layout says where one framework keeps the files the doctor inspects.
type layout struct {
	mcpPath   func(projectRoot string) (string, error) // MCP config file
	rulesRel  string                                   // project-relative rules file
	rulesKind string                                   // rulesBlock or rulesManaged
	frontmat  string                                   // managed file frontmatter
	location  string                                   // server location in the rules intro
}

const (
	rulesBlock   = "block"
	rulesManaged = "managed"
)

func projectPath(rel string) func(string) (string, error) {
	return func(root string) (string, error) { return filepath.Join(root, rel), nil }
}

// layouts covers every framework whose setup the doctor can verify.
var layouts = map[string]layout{
	FrameworkClaudeCode: {projectPath(projectMCPFile), claudeMDFile, rulesBlock, "", claudeCodeLocation},
	FrameworkCursor:     {projectPath(cursorMCPFile), cursorRulesFile, rulesManaged, cursorFrontmatter, cursorLocation},
	FrameworkWindsurf:   {projectPath(devinMCPFile), windsurfRulesFile, rulesManaged, windsurfFrontmatter, windsurfLocation},
	FrameworkCodex:      {projectPath(codexConfigFile), agentsMDFile, rulesBlock, "", agentsMDServerNote},
	FrameworkCopilot:    {projectPath(projectMCPFile), copilotRulesFile, rulesManaged, copilotFrontmatter, copilotLocation},
}

// Diagnose reports, for every framework that appears to be set up in the
// project, whether its MCP config, rules and hook agree and will work.
// Frameworks with none of their files present are left out.
func Diagnose(projectRoot string) []AgentReport {
	var reports []AgentReport
	for _, fw := range SupportedFrameworks {
		l, ok := layouts[fw]
		if !ok || !detected(projectRoot, fw, l) {
			continue
		}
		reports = append(reports, AgentReport{Framework: fw, Checks: diagnose(projectRoot, fw, l)})
	}
	return reports
}

// detected reports whether any of the framework's project files exist.
// AGENTS.md alone does not mark Codex as set up — generic writes it too.
func detected(projectRoot, fw string, l layout) bool {
	if fw != FrameworkCodex && fileExists(filepath.Join(projectRoot, l.rulesRel)) {
		return true
	}
	if fw == FrameworkCopilot {
		return false // its .mcp.json is shared with Claude Code
	}
	p, _ := l.mcpPath(projectRoot)
	return fileExists(p)
}

func diagnose(projectRoot, fw string, l layout) []Check {
	mcpFile, _ := l.mcpPath(projectRoot)
	entry := readServerEntry(fw, mcpFile)
	tier := tierOf(entry)

	checks := []Check{checkServer(fw, projectRoot, mcpFile, entry)}
	checks = append(checks, checkRules(projectRoot, fw, l, tier))
	checks = append(checks, checkSharing(projectRoot, fw, l))
	return append(checks, checkHook(projectRoot, fw))
}

// serverEntry is the parts of an "avc" MCP entry the doctor looks at.
type serverEntry struct {
	command string
	args    []string
}

// readServerEntry returns the framework's "avc" entry, or nil when absent.
func readServerEntry(fw, path string) *serverEntry {
	if fw == FrameworkCodex {
		var cfg codexConfig
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return nil
		}
		s, ok := cfg.MCPServers["avc"]
		if !ok {
			return nil
		}
		return &serverEntry{command: s.Command, args: s.Args}
	}

	root, existed, err := readJSONObject(path)
	if err != nil || !existed {
		return nil
	}
	servers, _ := root[keyMCPServers].(map[string]any)
	raw, ok := servers["avc"].(map[string]any)
	if !ok {
		return nil
	}
	e := &serverEntry{}
	e.command, _ = raw["command"].(string)
	if args, ok := raw["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				e.args = append(e.args, s)
			}
		}
	}
	return e
}

// tierOf returns the --tools value in an entry's args, or the default tier.
func tierOf(e *serverEntry) string {
	if e != nil {
		for i, a := range e.args {
			if a == "--tools" && i+1 < len(e.args) {
				return e.args[i+1]
			}
		}
	}
	return defaultToolTier
}

func checkServer(fw, projectRoot, path string, e *serverEntry) Check {
	c := Check{Name: "mcp server"}
	switch {
	case e == nil:
		c.Status, c.Detail = StatusFail, "no avc entry in "+path+" — run avc init --skills "+fw
	case !commandRuns(e.command):
		c.Status, c.Detail = StatusFail, "command "+e.command+" not found — install avc on PATH and re-run avc init --skills "+fw
	default:
		c.Status, c.Detail = StatusOK, e.command+" mcp serve, tool tier "+tierOf(e)
	}
	return c
}

// commandRuns reports whether command names an executable: a path that
// exists, or a bare name found on PATH.
func commandRuns(command string) bool {
	if command == "" {
		return false
	}
	if filepath.IsAbs(command) {
		return fileExists(command)
	}
	_, err := exec.LookPath(command)
	return err == nil
}

// checkRules compares the rules on disk with what init would write for the
// configured tool tier, so rules that name tools the server does not expose
// (or omit ones it does) are caught.
func checkRules(projectRoot, fw string, l layout, tier string) Check {
	c := Check{Name: "rules"}
	rel := filepath.ToSlash(l.rulesRel)
	data, err := os.ReadFile(filepath.Join(projectRoot, l.rulesRel))
	if err != nil {
		c.Status, c.Detail = StatusFail, rel+" is missing — run avc init --skills "+fw
		return c
	}
	content := string(data)

	var current bool
	if l.rulesKind == rulesBlock {
		start, end, ok := findBlock(content)
		if !ok {
			c.Status, c.Detail = StatusFail, rel+" has no AVC section — run avc init --skills "+fw
			return c
		}
		current = content[start:end] == strings.TrimSuffix(renderBlock(l.location, tier), "\n")
	} else {
		current = content == renderManagedFile(l.frontmat, l.location, tier)
	}

	if current {
		c.Status, c.Detail = StatusOK, rel+" matches tool tier "+tier
	} else {
		c.Status, c.Detail = StatusWarn, rel+" is out of date or edited (tool tier "+tier+") — run avc init --skills "+fw+" to refresh it"
	}
	return c
}

// checkSharing reports whether the framework's project files are gitignored,
// which keeps teammates from getting the setup.
func checkSharing(projectRoot, fw string, l layout) Check {
	c := Check{Name: "sharing"}
	ignored := gitignoredEntries(projectRoot)
	if ignored[filepath.ToSlash(l.rulesRel)] {
		c.Status, c.Detail = StatusInfo, filepath.ToSlash(l.rulesRel)+" is gitignored — teammates will not get this setup"
		return c
	}
	c.Status, c.Detail = StatusOK, "agent files are trackable"
	return c
}

// gitignoredEntries returns the exact entries listed in the project's
// .gitignore. It does not evaluate patterns — AVC writes exact paths.
func gitignoredEntries(projectRoot string) map[string]bool {
	entries := map[string]bool{}
	f, err := os.Open(filepath.Join(projectRoot, ".gitignore"))
	if err != nil {
		return entries
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		entries[strings.TrimSpace(s.Text())] = true
	}
	return entries
}

func checkHook(projectRoot, fw string) Check {
	c := Check{Name: "pre-edit hook"}
	installed, available, rel := hookInstalled(projectRoot, fw)
	switch {
	case !available:
		c.Status, c.Detail = StatusInfo, "not available for "+fw
	case installed:
		c.Status, c.Detail = StatusOK, "installed in "+filepath.ToSlash(rel)
	default:
		c.Status, c.Detail = StatusInfo, "not installed — avc init --skills "+fw+" --hooks adds a checkpoint before each session's first edit"
	}
	return c
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
