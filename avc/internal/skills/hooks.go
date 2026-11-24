// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package skills

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// preEditCommand is the hook command every harness runs. It reads the
// harness's JSON payload on stdin and always exits 0, so it can never block or
// fail the agent's edit — important for harnesses where a non-zero exit code
// vetoes the action.
const preEditCommand = "avc hook pre-edit"

// hookCommandFor returns the hook command for framework. Naming the harness
// attributes the checkpoint to it and selects the stdout the harness expects
// (Cursor needs a JSON permission answer; the others need silence).
func hookCommandFor(framework string) string {
	return preEditCommand + " --agent " + framework
}

// hookSpec describes one hooks file a harness reads and the AVC entry in it.
type hookSpec struct {
	rel string // project-relative hooks file
	// unwrapped means the file's top level is the event map itself (Devin's
	// hooks.v1.json); otherwise events sit under a top-level "hooks" key.
	unwrapped bool
	topKeys   map[string]any // other top-level keys the format requires
	event     string         // event name
	entry     any            // the entry appended to that event's list
}

// hookFiles describes, per framework, the hooks files to install the pre-edit
// checkpoint into, and a caveat to show the user. Frameworks absent here have
// no project-level hook that runs before an edit.
//
// Formats follow each tool's documentation (November 2026):
//   - Claude Code: .claude/settings.json, PreToolUse matching Write|Edit.
//   - Windsurf, now Devin Desktop: .devin/hooks.v1.json holds the event map
//     directly, Claude-style PreToolUse groups. The legacy Windsurf
//     .windsurf/hooks.json pre_write_code hook is kept for older versions.
//   - Codex: .codex/hooks.json; apply_patch edits match apply_patch|Edit|Write.
//   - Copilot: .github/hooks/*.json, "version": 1.
//   - Cursor: .cursor/hooks.json, "version": 1; preToolUse matching Write is
//     the only event before an edit (afterFileEdit runs too late).
var hookFiles = map[string]struct {
	specs []hookSpec
	note  string
}{
	FrameworkClaudeCode: {specs: []hookSpec{{
		rel:   filepath.Join(".claude", "settings.json"),
		event: "PreToolUse",
		entry: map[string]any{
			"matcher": "Write|Edit",
			"hooks":   []any{map[string]any{"type": "command", "command": preEditCommand}},
		},
	}}},
	FrameworkWindsurf: {specs: []hookSpec{
		{
			rel:       filepath.Join(".devin", "hooks.v1.json"),
			unwrapped: true,
			event:     "PreToolUse",
			entry: map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": hookCommandFor(FrameworkWindsurf)}},
			},
		},
		{
			rel:   filepath.Join(".windsurf", "hooks.json"),
			event: "pre_write_code",
			entry: map[string]any{"command": hookCommandFor(FrameworkWindsurf), "show_output": false},
		},
	}},
	FrameworkCodex: {
		specs: []hookSpec{{
			rel:   filepath.Join(".codex", "hooks.json"),
			event: "PreToolUse",
			entry: map[string]any{
				"matcher": "apply_patch|Edit|Write",
				"hooks":   []any{map[string]any{"type": "command", "command": hookCommandFor(FrameworkCodex)}},
			},
		}},
		note: "Codex loads project hooks only for trusted projects, and asks you to review new hooks once — run /hooks in Codex to trust it.",
	},
	FrameworkCopilot: {
		specs: []hookSpec{{
			rel:     filepath.Join(".github", "hooks", "avc.json"),
			topKeys: map[string]any{"version": 1},
			event:   "PreToolUse",
			entry:   map[string]any{"type": "command", "command": hookCommandFor(FrameworkCopilot)},
		}},
		note: "Copilot reads .github/hooks/avc.json in VS Code agent sessions, the Copilot CLI and the coding agent; VS Code's agent hooks may still be in preview in your build.",
	},
	FrameworkCursor: {specs: []hookSpec{{
		rel:     filepath.Join(".cursor", "hooks.json"),
		topKeys: map[string]any{"version": 1},
		event:   "preToolUse",
		entry:   map[string]any{"command": hookCommandFor(FrameworkCursor), "matcher": "Write"},
	}}},
}

// writeHook installs the AVC pre-edit hook for framework into every hooks
// file it reads, merging without disturbing other hooks.
func writeHook(projectRoot, framework string, r *WriteResult) error {
	files, ok := hookFiles[framework]
	if !ok {
		r.Warnings = append(r.Warnings, framework+" has no project-level pre-edit hook; --hooks was ignored for it")
		return nil
	}
	installed := false
	for _, spec := range files.specs {
		action, err := installHook(projectRoot, spec)
		if err != nil {
			return err
		}
		r.Actions = append(r.Actions, fileAction(filepath.ToSlash(spec.rel), action))
		installed = installed || action != skipAlreadyConfigured
	}
	if installed && files.note != "" {
		r.Warnings = append(r.Warnings, files.note)
	}
	return nil
}

// installHook merges one AVC hook entry into one hooks file.
func installHook(projectRoot string, spec hookSpec) (string, error) {
	path := filepath.Join(projectRoot, spec.rel)
	root, existed, err := readJSONObject(path)
	if err != nil {
		return "", err
	}
	for k, v := range spec.topKeys {
		if _, set := root[k]; !set {
			root[k] = v
		}
	}
	events := eventMap(root, spec)
	list, _ := events[spec.event].([]any)
	if containsPreEdit(list) {
		return skipAlreadyConfigured, nil
	}
	events[spec.event] = append(list, spec.entry)

	if err := writeJSON(path, root); err != nil {
		return "", err
	}
	if existed {
		return actionUpdated, nil
	}
	return actionCreated, nil
}

// eventMap returns the map holding a hooks file's events, creating the
// "hooks" key when the format wraps events in one.
func eventMap(root map[string]any, spec hookSpec) map[string]any {
	if spec.unwrapped {
		return root
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	return hooks
}

// hookInstalled reports whether a framework's primary hooks file already runs
// the AVC command, and names that file.
func hookInstalled(projectRoot, framework string) (installed, available bool, rel string) {
	files, ok := hookFiles[framework]
	if !ok || len(files.specs) == 0 {
		return false, false, ""
	}
	spec := files.specs[0]
	root, _, err := readJSONObject(filepath.Join(projectRoot, spec.rel))
	if err != nil {
		return false, true, spec.rel
	}
	list, _ := eventMap(root, spec)[spec.event].([]any)
	return containsPreEdit(list), true, spec.rel
}

// containsPreEdit reports whether any hook entry already runs the AVC command.
func containsPreEdit(list []any) bool {
	for _, e := range list {
		data, err := json.Marshal(e)
		if err == nil && strings.Contains(string(data), preEditCommand) {
			return true
		}
	}
	return false
}
