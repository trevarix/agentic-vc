// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fileURI renders a local directory as the file:// URI a client would send.
func fileURI(dir string) string {
	p := filepath.ToSlash(dir)
	if runtime.GOOS == "windows" {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// decodeAll splits the server's output into its JSON messages.
func decodeAll(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var msgs []map[string]any
	dec := json.NewDecoder(out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// TestServeSelectsProjectFromClientRoots covers a host with one global server
// config (Windsurf, Codex): the server starts outside any project, asks the
// client for its workspace folders, and adopts the project they lie in.
func TestServeSelectsProjectFromClientRoots(t *testing.T) {
	project := mkProj(t, filepath.Join(t.TempDir(), "app"))
	sub := filepath.Join(project, "src")

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"roots":{"listChanged":true}}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"avc-roots-1","result":{"roots":[{"uri":"` + fileURI(sub) + `"}]}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"avc_projects_list","arguments":{}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := serve(strings.NewReader(input), &out, "", nil, false, "standard"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	msgs := decodeAll(t, &out)

	var sawRequest, sawListChanged bool
	for _, m := range msgs {
		if m["method"] == methodRootsList {
			sawRequest = true
		}
		if m["method"] == methodToolsListChanged {
			sawListChanged = true
		}
		if _, isResponse := m["id"]; isResponse && m["method"] == nil && m["id"] == "avc-roots-1" {
			t.Errorf("the server answered the client's roots response: %v", m)
		}
	}
	if !sawRequest {
		t.Error("server never sent roots/list to a client that supports roots")
	}
	if !sawListChanged {
		t.Error("server did not announce the tool list change after adopting a project")
	}

	last := msgs[len(msgs)-1]
	text := last["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	resolved, _ := filepath.EvalSymlinks(project)
	if !strings.Contains(text, `"current": true`) || !strings.Contains(text, filepath.Base(resolved)) {
		t.Errorf("project from client roots was not selected: %s", text)
	}
}

// TestServeNoRootsRequestWithoutCapability keeps clients that never declared
// roots support free of requests they cannot answer.
func TestServeNoRootsRequestWithoutCapability(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	var out bytes.Buffer
	if err := serve(strings.NewReader(input), &out, "", nil, false, "standard"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if strings.Contains(out.String(), methodRootsList) {
		t.Errorf("roots/list sent to a client without the roots capability: %s", out.String())
	}
}

// TestPinnedSessionIgnoresClientRoots preserves the project-bound contract: a
// server started inside a project keeps acting on it.
func TestPinnedSessionIgnoresClientRoots(t *testing.T) {
	pinned := mkProj(t, filepath.Join(t.TempDir(), "pinned"))
	other := mkProj(t, filepath.Join(t.TempDir(), "other"))

	s := newSession(pinned, nil)
	if s.applyClientRoots([]string{other}) {
		t.Error("a pinned session reported a change from client roots")
	}
	if s.current != pinned {
		t.Errorf("current = %q, want pinned %q", s.current, pinned)
	}
}

// TestClientRootsSpanningProjectsDoNotGuess leaves the choice to the agent
// when the open folders lie in more than one project.
func TestClientRootsSpanningProjectsDoNotGuess(t *testing.T) {
	a := mkProj(t, filepath.Join(t.TempDir(), "a"))
	b := mkProj(t, filepath.Join(t.TempDir(), "b"))

	s := newSession("", nil)
	s.applyClientRoots([]string{a, b})
	if s.current != "" {
		t.Errorf("picked %q from two projects; the agent should choose", s.current)
	}
	if len(s.roots) != 2 {
		t.Errorf("roots = %v, want both projects searchable", s.roots)
	}
}
