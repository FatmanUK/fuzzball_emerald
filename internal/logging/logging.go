// Package logging configures the server's structured logger.
//
// Fuzzball 7 wrote a dozen separate log files under logs/, chosen by the
// file_log_* tune parameters. Emerald writes one structured stream and tags
// each record with the channel the old server would have used, so operators
// can split it back apart with whatever they already run.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Channel names the log stream a record belongs to. They mirror the
// file_log_* parameters so existing operational habits still transfer.
type Channel string

const (
	Status   Channel = "status"   // server lifecycle
	Command  Channel = "command"  // player commands
	Program  Channel = "program"  // program compiles and edits
	MUFError Channel = "muferror" // MUF runtime errors
	Gripe    Channel = "gripe"    // player gripes
	Sanity   Channel = "sanity"   // database consistency
	Muf      Channel = "muf"      // MUF diagnostics
)

// New builds a logger writing to w.
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info", "":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("unknown log level %q", level)
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	switch strings.ToLower(format) {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	case "text", "":
		h = slog.NewTextHandler(w, opts)
	default:
		return nil, fmt.Errorf("unknown log format %q", format)
	}
	return slog.New(h), nil
}

// On returns a logger tagged with a channel.
func On(l *slog.Logger, c Channel) *slog.Logger {
	return l.With(slog.String("channel", string(c)))
}
