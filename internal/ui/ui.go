// Package ui provides the small colored logging helpers used across ssmgate.
// Colors are emitted only when stderr is a TTY (and never on Windows legacy
// consoles that do not support ANSI), keeping piped/CI output clean.
package ui

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/term"
)

// Out is where status lines are written. Tests redirect it.
var Out io.Writer = os.Stderr

var useColor = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// Windows legacy terminals may not handle ANSI; be conservative.
	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("TERM") == "" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

func color(code, s string) string {
	if !useColor {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// Log prints an informational line (blue tag).
func Log(format string, a ...any) {
	_, _ = fmt.Fprintf(Out, "%s %s\n", color("1;34", "[info ]"), fmt.Sprintf(format, a...))
}

// OK prints a success line (green tag).
func OK(format string, a ...any) {
	_, _ = fmt.Fprintf(Out, "%s %s\n", color("1;32", "[ ok  ]"), fmt.Sprintf(format, a...))
}

// Warn prints a warning line (yellow tag).
func Warn(format string, a ...any) {
	_, _ = fmt.Fprintf(Out, "%s %s\n", color("1;33", "[warn ]"), fmt.Sprintf(format, a...))
}

// Errorf builds an error decorated with the red [error] tag for consistent
// terminal output when surfaced at the top level.
func Errorf(format string, a ...any) error {
	return fmt.Errorf("%s %s", color("1;31", "[error]"), fmt.Sprintf(format, a...))
}
