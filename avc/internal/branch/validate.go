package branch

import (
	"fmt"
	"regexp"
	"strings"
)

// validBranchNameRe allows letters, digits, -, _, /, and . — the same
// characters Git permits in branch names (excluding the refs/ prefix).
// Length is capped at 100 characters so branch names are usable as directory
// names without hitting OS path-length limits.
var validBranchNameRe = regexp.MustCompile(`^[a-zA-Z0-9._/\-]{1,100}$`)

// windowsReserved is the set of Windows device names that cannot be used as
// directory names on any Windows filesystem.
var windowsReserved = map[string]bool{
	"con":  true,
	"prn":  true,
	"aux":  true,
	"nul":  true,
	"com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateBranchName returns a non-nil error when name is not safe to use as
// an AVC branch name or as a workspace directory name on disk.
//
// Rules:
//   - Not empty
//   - Not "main" (reserved for the primary branch)
//   - Not a Windows reserved device name (con, nul, com1…)
//   - Only letters, digits, -, _, /, . (no spaces, colons, or shell-special chars)
//   - Does not start or end with "."
//   - Does not contain ".." (path traversal guard)
//   - Max 100 characters
func ValidateBranchName(name string) error {
	if name == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if name == "main" {
		return fmt.Errorf("'main' is a reserved branch name")
	}
	if windowsReserved[strings.ToLower(name)] {
		return fmt.Errorf("'%s' is a reserved system name and cannot be used as a branch name", name)
	}
	if !validBranchNameRe.MatchString(name) {
		return fmt.Errorf(
			"branch name %q contains illegal characters; "+
				"use only letters, digits, -, _, /, and .",
			name,
		)
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("branch name must not start or end with '.'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("branch name must not contain '..' (path traversal guard)")
	}
	return nil
}
