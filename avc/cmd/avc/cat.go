// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/trevarix/agentic-vc/avc/internal/snapshot"
	"github.com/spf13/cobra"
)

var catCmd = &cobra.Command{
	Use:   "cat <snapshot_id> <file_path>",
	Short: "Print the contents of a file from a snapshot to stdout",
	Args:  cobra.ExactArgs(2),
	RunE:  runCat,
}

func init() {
	rootCmd.AddCommand(catCmd)
}

func runCat(cmd *cobra.Command, args []string) error {
	snapshotID := args[0]
	filePath := args[1]

	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	data, err := snapshot.CatFile(projectPath, snapshotID, filePath)
	if err != nil {
		return fmt.Errorf("cat failed: %w", err)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"snapshot_id":    snapshotID,
			"file_path":      filePath,
			"size":           len(data),
			"content_base64": base64.StdEncoding.EncodeToString(data),
		})
	}

	// When stdout is a pipe or redirect, output raw bytes so the content is
	// usable by other tools (e.g. avc cat snap-x file.go > out.go).
	if !colorsEnabled {
		_, err = os.Stdout.Write(data)
		return err
	}

	// Interactive terminal: render with a header and line numbers.
	displayPath := filepath.ToSlash(filepath.Clean(filePath))
	text := decodeFileBytes(data)

	// Normalise line endings then split, removing all trailing empty lines.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	fmt.Printf("%s %s  %s\n", accent("◆"), cyan(displayPath), dim(snapshotID))
	fmt.Println(ruler(60))
	for i, line := range lines {
		fmt.Printf("%s  %s\n", dim(fmt.Sprintf("%4d", i+1)), line)
	}
	fmt.Println()
	return nil
}

// decodeFileBytes converts raw file bytes to a displayable UTF-8 string.
// Handles UTF-8 BOM, UTF-16 LE, and UTF-16 BE — all written by Windows tools.
func decodeFileBytes(data []byte) string {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:]) // UTF-8 BOM — content is already UTF-8
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		// UTF-16 LE — decode pairs little-endian
		u16 := make([]uint16, (len(data)-2)/2)
		for i := range u16 {
			u16[i] = uint16(data[2+2*i]) | uint16(data[2+2*i+1])<<8
		}
		return string(utf16.Decode(u16))
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		// UTF-16 BE — decode pairs big-endian
		u16 := make([]uint16, (len(data)-2)/2)
		for i := range u16 {
			u16[i] = uint16(data[2+2*i])<<8 | uint16(data[2+2*i+1])
		}
		return string(utf16.Decode(u16))
	}
	return string(data) // plain UTF-8 or ASCII
}
