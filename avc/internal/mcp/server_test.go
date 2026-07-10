// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadLine_NormalLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("hello\nworld\n"))
	line, tooLong, err := readLine(r)
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if tooLong {
		t.Error("expected tooLong=false for a normal line")
	}
	if string(line) != "hello\n" {
		t.Errorf("line = %q, want %q", line, "hello\n")
	}
}

// TestReadLine_OversizedLineIsDiscardedAndResyncs verifies the fix for the
// review's finding that bufio.Scanner permanently stops the whole MCP
// session the first time one line exceeds its fixed buffer. readLine must
// instead discard the oversized line and resynchronize cleanly at the next one.
func TestReadLine_OversizedLineIsDiscardedAndResyncs(t *testing.T) {
	huge := strings.Repeat("x", maxLineBytes+1000)
	input := huge + "\nnormal\n"
	r := bufio.NewReader(strings.NewReader(input))

	line, tooLong, err := readLine(r)
	if err != nil {
		t.Fatalf("readLine (oversized): %v", err)
	}
	if !tooLong {
		t.Fatal("expected tooLong=true for a line exceeding maxLineBytes")
	}
	if len(line) != 0 {
		t.Errorf("expected no returned content for a discarded oversized line, got %d bytes", len(line))
	}

	line2, tooLong2, err2 := readLine(r)
	if err2 != nil {
		t.Fatalf("readLine (after resync): %v", err2)
	}
	if tooLong2 {
		t.Error("expected the line following the oversized one to be reported normally")
	}
	if string(line2) != "normal\n" {
		t.Errorf("line2 = %q, want %q", line2, "normal\n")
	}
}

// TestServe_SurvivesOversizedRequestAndKeepsServing is the end-to-end version
// of the fix: a request too large to buffer must not kill the session — the
// server should report an error for it and continue serving the next request.
func TestServe_SurvivesOversizedRequestAndKeepsServing(t *testing.T) {
	huge := strings.Repeat("x", maxLineBytes+1000)
	pingReq := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	input := huge + "\n" + pingReq

	var out bytes.Buffer
	if err := serve(strings.NewReader(input), &out, "", false, "standard"); err != nil {
		t.Fatalf("serve: %v", err)
	}

	dec := json.NewDecoder(&out)
	var responses []map[string]any
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, m)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses (error for the oversized line, then a pong), got %d: %+v", len(responses), responses)
	}
	if _, hasError := responses[0]["error"]; !hasError {
		t.Errorf("expected the first response to be an error for the oversized line, got %+v", responses[0])
	}
	if _, hasResult := responses[1]["result"]; !hasResult {
		t.Errorf("expected the second response to be a successful result for ping, got %+v", responses[1])
	}
}
