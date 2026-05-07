// Package mcp implements a Model Context Protocol (MCP) server over stdio.
// The server exposes AVC operations as MCP tools so any MCP-capable agent
// (Claude Code, Cursor, Cline, Windsurf, etc.) can snapshot, diff, and restore
// without leaving its native workflow.
//
// Protocol: JSON-RPC 2.0, newline-delimited, over stdin/stdout.
// Spec version: 2024-11-05.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "avc"
	serverVersion   = "0.1.0"
)

// mcpInstructions is returned in the MCP initialize response and automatically
// injected into the agent's context by MCP-capable clients (Claude Desktop,
// Claude Code, Cursor, Windsurf). This ensures the agent knows how to use AVC
// without requiring a separate CLAUDE.md or rules file.
const mcpInstructions = `You are working in a project that uses AVC (Agentic Version Control). You MUST follow these rules without exception.

REQUIRED SEQUENCE — follow this order for every task:
START:
1. Call avc_branch_create to get an isolated workspace.
2. Read the "workspace" path from the response. ALL file reads and writes go to that path — never the original project root.
3. Always Read files from the workspace path before editing — do not reuse content read from the project root.
4. Call avc_snapshot (label: "auto: before <action>") before touching any files.
Then make your changes.
FINISH:
5. Call avc_snapshot to capture your edits (label: "auto: before <action>" is fine — it just needs to be the current state).
6. Call avc_branch_diff and show the full output to the user.
7. Ask the user: "Shall I merge this into main?" — wait for explicit yes.
8. Call avc_merge. It checks for conflicts automatically; if any are found it returns them without writing anything.

BRANCHES
- Call avc_branch_create before starting any task. No exceptions.
- Do not assess whether the task is "simple enough" to skip — that judgement is not yours to make.
- The response contains "workspace": use that exact path for every file operation. NEVER touch the real project root.

SNAPSHOTS
- Call avc_snapshot before making any code change. No exceptions.
- Call avc_snapshot again after finishing your edits, before calling avc_branch_diff — the diff compares snapshots, so unsaved edits won't appear until captured.
- Label format: "auto: before <action>" or "auto: after <action>" (2–5 words). Example: "auto: before auth refactor".
- Always provide agent_name (e.g. "claude") and notes describing the planned change.

RESTORE
- Call avc_restore immediately when tests fail, the build breaks, or the user says "undo" or "roll back".
- Do NOT attempt repeated fixes on broken state — restore first, then retry.

MERGE
- NEVER call avc_merge without the user explicitly saying yes.
- avc_merge checks for conflicts automatically before writing anything — no separate preview step needed.
- If the merge response contains an "error" field, conflicts were detected. Show them to the user and ask how to resolve before retrying.
- If anything goes wrong, call avc_merge_abort immediately.

RUNNING COMMANDS
- NEVER call avc_run_in_workspace without first stating the exact command to the user, explaining what it does, and receiving explicit approval.
- System package managers (brew, apt, choco, sudo) are blocked.
- Python: use pip install (no --user). Node: use npm install (no -g or --global).`

// rpcRequest is an incoming JSON-RPC 2.0 message.
// Notifications have no id field; requests always have one.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil/absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is an outgoing JSON-RPC 2.0 message.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve runs the MCP server over stdin/stdout, blocking until EOF.
//
// projectRoot is the resolved AVC project root directory (.avc/ parent).
// compact controls whether tool output JSON is compact or pretty-printed.
func Serve(projectRoot string, compact bool) error {
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	// 4 MB buffer — large diffs can produce substantial JSON output.
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeErrorNoID(enc, -32700, "parse error: "+err.Error())
			continue
		}

		// Notifications have no id — they require no response.
		if len(req.ID) == 0 {
			continue
		}

		result, rpcErr := dispatch(projectRoot, compact, req.Method, req.Params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp)
	}

	return scanner.Err()
}

// dispatch routes a method to its handler.
func dispatch(projectRoot string, compact bool, method string, rawParams json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"instructions":    mcpInstructions,
		}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": AllTools()}, nil

	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(rawParams, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		result, err := dispatchTool(projectRoot, compact, p.Name, p.Arguments)
		if err != nil {
			return nil, &rpcError{Code: -32000, Message: err.Error()}
		}
		return result, nil

	default:
		return nil, &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", method)}
	}
}

// writeErrorNoID writes a JSON-RPC error without an id (used for parse errors).
func writeErrorNoID(enc *json.Encoder, code int, msg string) {
	_ = enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error":   map[string]any{"code": code, "message": msg},
	})
}

// wrapContent wraps a value in the MCP tool content envelope.
// If v is already a string it is used as-is (pre-formatted text).
// Otherwise v is marshalled to JSON (compact or pretty-printed).
// SetEscapeHTML(false) prevents diff characters like < > & from being
// mangled into < etc., which makes diff output unreadable.
func wrapContent(v any, compact bool) (map[string]any, error) {
	var text string
	if s, ok := v.(string); ok {
		text = s
	} else {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if !compact {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		// json.Encoder always appends a trailing newline; trim it.
		text = string(bytes.TrimRight(buf.Bytes(), "\n"))
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}, nil
}
