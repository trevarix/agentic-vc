package main

import (
	"fmt"
	"os"

	"github.com/trevarix/agentic-vc/avc/cmd/avc"
)

// version, commit, and date are injected at build time via -ldflags:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=abc1234 -X main.date=2026-05-24"
//
// The release workflow sets these automatically on every tagged release.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	avc.SetVersion(version, commit, date)
	if err := avc.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
