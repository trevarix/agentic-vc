// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

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
	"io"
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

// maxLineBytes bounds a single JSON-RPC request line. Requests larger than
// this are discarded (not buffered in full) rather than accepted unbounded.
const maxLineBytes = 32 * 1024 * 1024

// Serve runs the MCP server over stdin/stdout, blocking until EOF.
//
// projectRoot is the resolved AVC project root directory (.avc/ parent).
// compact controls whether tool output JSON is compact or pretty-printed.
// toolTier selects the advertised tool set: "core", "standard" (default), or "full".
func Serve(projectRoot string, compact bool, toolTier string) error {
	return serve(os.Stdin, os.Stdout, projectRoot, compact, toolTier)
}

// serve is Serve with the transport as parameters, so it can be exercised
// with an in-memory pipe in tests.
func serve(r io.Reader, w io.Writer, projectRoot string, compact bool, toolTier string) error {
	enc := json.NewEncoder(w)
	reader := bufio.NewReaderSize(r, 64*1024)

	for {
		line, tooLong, err := readLine(reader)

		switch {
		case tooLong:
			// A request this large is almost certainly a mistake (or a
			// large avc_resolve_conflict content payload exceeding the
			// cap) rather than a reason to kill the whole MCP session —
			// unlike bufio.Scanner, which stops permanently the first time
			// a single token exceeds its fixed buffer. Report and keep
			// serving; readLine has already resynchronized to the next line.
			writeErrorNoID(enc, -32600,
				fmt.Sprintf("request exceeds the %d byte limit and was discarded; the connection is still open", maxLineBytes))
		case len(bytes.TrimRight(line, "\r\n")) > 0:
			trimmed := bytes.TrimRight(line, "\r\n")
			var req rpcRequest
			if jsonErr := json.Unmarshal(trimmed, &req); jsonErr != nil {
				writeErrorNoID(enc, -32700, "parse error: "+jsonErr.Error())
			} else if len(req.ID) != 0 {
				// Notifications have no id — they require no response.
				result, rpcErr := dispatch(projectRoot, compact, toolTier, req.Method, req.Params)
				resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
				if rpcErr != nil {
					resp.Error = rpcErr
				} else {
					resp.Result = result
				}
				_ = enc.Encode(resp)
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// readLine reads one newline-terminated line from r, in bounded-size chunks
// via ReadSlice so no single Read call must buffer an entire oversized line.
// If the accumulated line exceeds maxLineBytes, remaining bytes up to (and
// including) the next newline are read and discarded — not returned — so the
// stream resynchronizes cleanly at the next request, and tooLong is reported
// so the caller can respond with an error instead of silently dropping data.
//
// This replaces bufio.Scanner, which permanently stops the entire session
// (Scan always returns false afterward) the first time one token exceeds its
// fixed buffer — an unrecoverable failure for what should be a per-request problem.
func readLine(r *bufio.Reader) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, e := r.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > maxLineBytes {
				tooLong = true
				buf = nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if e == nil {
			return buf, tooLong, nil // found the newline
		}
		if e != bufio.ErrBufferFull {
			// Real error (EOF, underlying read error) with a partial,
			// unterminated final line.
			return buf, tooLong, e
		}
		// ErrBufferFull: ReadSlice's internal buffer filled without finding
		// '\n' yet — loop again, accumulating (or discarding) further chunks.
	}
}

// dispatch routes a method to its handler.
func dispatch(projectRoot string, compact bool, toolTier string, method string, rawParams json.RawMessage) (any, *rpcError) {
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
		// When no AVC project is detected, expose an empty tool set so the agent
		// cannot misuse snapshot/branch/merge tools on an uninitialised directory.
		if projectRoot == "" {
			return map[string]any{"tools": ProjectlessTools()}, nil
		}
		return map[string]any{"tools": ToolsForTier(toolTier)}, nil

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
