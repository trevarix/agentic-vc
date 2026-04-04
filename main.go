package main

import (
	"fmt"
	"os"

	"github.com/agentic-version-ctl/avc/cmd/avc"
)

func main() {
	if err := avc.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
