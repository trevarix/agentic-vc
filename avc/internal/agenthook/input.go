// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthook

import (
	"bytes"
	"encoding/json"
	"path/filepath"
)

// Field names that carry the session identifier, in the order they are tried.
// Claude Code, Codex and VS Code use session_id; Copilot's camelCase payloads
// use sessionId; Windsurf identifies a Cascade conversation by trajectory_id;
// Cursor uses conversation_id.
var sessionKeys = []string{"session_id", "sessionId", "trajectory_id", "conversation_id", "conversationId"}

// Field names that carry the working directory.
var cwdKeys = []string{"cwd", "workspaceFolder", "workspace_folder"}

// Objects whose file path locates the project when no working directory is
// sent (Windsurf's pre_write_code puts the target file under tool_info).
var toolObjectKeys = []string{"tool_info", "tool_input", "toolInput"}

// File-path fields inside those objects.
var filePathKeys = []string{"file_path", "filePath", "path"}

// ParseInput decodes a hook payload from any supported harness into an Input.
//
// Harnesses name the same facts differently, so each field is looked up under
// every known spelling. When the payload has no working directory, the
// directory of the file being edited stands in for it — that is enough to
// find the enclosing AVC project. Empty input yields an empty Input, which
// PreEdit turns into an ordinary skip.
func ParseInput(data []byte) (Input, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Input{}, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Input{}, err
	}

	in := Input{
		SessionID: firstString(raw, sessionKeys),
		CWD:       firstString(raw, cwdKeys),
		ToolName:  firstString(raw, []string{"tool_name", "toolName", "agent_action_name"}),
	}
	if in.CWD == "" {
		if roots, ok := raw["workspace_roots"].([]any); ok && len(roots) > 0 {
			in.CWD, _ = roots[0].(string)
		}
	}
	if in.CWD == "" {
		if file := toolFilePath(raw); file != "" && filepath.IsAbs(file) {
			in.CWD = filepath.Dir(file)
		}
	}
	return in, nil
}

// toolFilePath returns the path of the file a tool call targets, if any.
func toolFilePath(raw map[string]any) string {
	for _, key := range toolObjectKeys {
		if obj, ok := raw[key].(map[string]any); ok {
			if p := firstString(obj, filePathKeys); p != "" {
				return p
			}
		}
	}
	return ""
}

// cursorAllow is Cursor's answer for its preToolUse permission hook. Cursor
// treats invalid JSON on stdout as a denial, so without it the hook would
// block the edit it exists to protect.
const cursorAllow = `{"permission":"allow"}`

// HarnessReply returns what the pre-edit hook must print on stdout for the
// harness that ran it, and whether the human-readable report must stay off
// stdout. Hooks run before a tool call, where harnesses read stdout as a
// decision: Cursor needs an explicit allow, and the others (Codex, Copilot,
// Devin/Windsurf) are safest given nothing. With no harness named — Claude
// Code's plugin hook — the report prints as usual.
func HarnessReply(harness string) (stdout string, quiet bool) {
	switch harness {
	case "":
		return "", false
	case "cursor":
		return cursorAllow, true
	default:
		return "", true
	}
}

// firstString returns the first non-empty string value among keys.
func firstString(m map[string]any, keys []string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
