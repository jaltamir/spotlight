// Package log provides a hybrid logging facade.
//
// INFO and WARN use pretty-printed stderr output (arrows/symbols).
// DEBUG uses slog structured logging and is only emitted when debug mode is on.
// All output goes to stderr.
package log

import (
	"fmt"
	"log/slog"
	"os"
)

var debugEnabled bool

// SetDebug enables or disables debug output.
func SetDebug(on bool) {
	debugEnabled = on
	if on {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}
}

// Info prints an informational message to stderr with a pretty arrow prefix.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "→ %s\n", msg)
}

// Infof prints a formatted informational message to stderr.
func Infof(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "→ "+format+"\n", args...)
}

// Warn prints a warning message to stderr.
func Warn(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ %s: %v\n", msg, err)
	} else {
		fmt.Fprintf(os.Stderr, "⚠ %s\n", msg)
	}
}

// Debug emits a structured debug log entry. Only active when debug mode is on.
// attrs should be key-value pairs: Debug("msg", "key", val, "key2", val2).
func Debug(msg string, attrs ...any) {
	if !debugEnabled {
		return
	}
	slog.Debug(msg, attrs...)
}
