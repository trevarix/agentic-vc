package main

import (
	"fmt"
	"os"

	"github.com/SkillMythOrg/agentic-vc/avc/cmd/avc"
)

func main() {
	if err := avc.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
