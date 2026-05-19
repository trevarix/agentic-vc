package avc

import (
	"os"
	"strings"
)

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
	ansiWhite   = "\033[37m"
)

// colorsEnabled is true when stdout is a terminal that supports ANSI sequences.
var colorsEnabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}()

func colorize(code, s string) string {
	if !colorsEnabled {
		return s
	}
	return code + s + ansiReset
}

// Base color helpers.
func bold(s string) string    { return colorize(ansiBold, s) }
func dim(s string) string     { return colorize(ansiDim, s) }
func red(s string) string     { return colorize(ansiRed, s) }
func green(s string) string   { return colorize(ansiGreen, s) }
func yellow(s string) string  { return colorize(ansiYellow, s) }
func blue(s string) string    { return colorize(ansiBlue, s) }
func magenta(s string) string { return colorize(ansiMagenta, s) }
func cyan(s string) string    { return colorize(ansiCyan, s) }

// Semantic helpers — use these for consistent meaning across commands.
func success(s string) string { return colorize(ansiBold+ansiGreen, s) }  // operation succeeded
func failure(s string) string { return colorize(ansiBold+ansiRed, s) }    // error / conflict
func warn(s string) string    { return colorize(ansiBold+ansiYellow, s) } // warning / needs attention
func accent(s string) string  { return colorize(ansiBold+ansiCyan, s) }   // titles and labels
func prop(s string) string    { return colorize(ansiBold+ansiWhite, s) }  // key names in key-value pairs

// ruler returns a dimmed horizontal rule of the given width.
func ruler(width int) string { return dim(strings.Repeat("─", width)) }
