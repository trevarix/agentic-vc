// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// MCP methods the server uses to learn the client's workspace folders.
const (
	methodInitialized      = "notifications/initialized"
	methodRootsList        = "roots/list"
	methodRootsListChanged = "notifications/roots/list_changed"
	methodToolsListChanged = "notifications/tools/list_changed"
)

// rootsRequestPrefix starts the id of every roots/list request this server
// sends, so the client's response can be told apart from anything else.
const rootsRequestPrefix = "avc-roots-"

// clientSupportsRoots reports whether initialize params declare the roots
// capability.
func clientSupportsRoots(rawParams json.RawMessage) bool {
	var p struct {
		Capabilities struct {
			Roots json.RawMessage `json:"roots"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(rawParams, &p) != nil {
		return false
	}
	return len(p.Capabilities.Roots) > 0 && string(p.Capabilities.Roots) != "null"
}

// rootsRequest builds the n-th roots/list request.
func rootsRequest(n int) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      fmt.Sprintf("%s%d", rootsRequestPrefix, n),
		"method":  methodRootsList,
	}
}

// isRootsResponse reports whether a response id belongs to a roots/list
// request this server sent.
func isRootsResponse(id json.RawMessage) bool {
	var s string
	return json.Unmarshal(id, &s) == nil && strings.HasPrefix(s, rootsRequestPrefix)
}

// parseRootsResult extracts local directory paths from a roots/list result.
// Roots that are not file:// URIs are skipped: the server can only search
// the local disk.
func parseRootsResult(result json.RawMessage) []string {
	var r struct {
		Roots []struct {
			URI string `json:"uri"`
		} `json:"roots"`
	}
	if json.Unmarshal(result, &r) != nil {
		return nil
	}
	var dirs []string
	for _, root := range r.Roots {
		if dir, ok := fileURIToPath(root.URI); ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// fileURIToPath converts a file:// URI to a local path. On Windows the URI
// path "/C:/work/app" becomes "C:\work\app".
func fileURIToPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
		if u.Host != "" && u.Host != "localhost" {
			p = `\\` + u.Host + `\` + p // UNC share
		}
	}
	if p == "" {
		return "", false
	}
	return canonicalPath(filepath.FromSlash(p)), true
}

// canonicalPath resolves symlinks and, on Windows, 8.3 short names
// (C:\Users\LONGNA~1), so a client-reported folder matches the path the
// project was registered under. A path that cannot be resolved is kept as is.
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
