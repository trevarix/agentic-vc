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
			"instructions":    buildInstructions(),
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
