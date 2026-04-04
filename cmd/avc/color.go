package avc

import "os"

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
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

func bold(s string) string   { return colorize(ansiBold, s) }
func dim(s string) string    { return colorize(ansiDim, s) }
func red(s string) string    { return colorize(ansiRed, s) }
func green(s string) string  { return colorize(ansiGreen, s) }
func yellow(s string) string { return colorize(ansiYellow, s) }
func cyan(s string) string   { return colorize(ansiCyan, s) }
