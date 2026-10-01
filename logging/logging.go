// Package logging builds the process logger: structured logs (slog) that go to
// stdout AND to a size-rotated file.
//
// stdout is not redundant: under systemd the journal is what `journalctl -u`
// reads, and a support person looks there first. The file is what survives a
// journal vacuum and what you can grep weeks later.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options configures the logger.
type Options struct {
	// Path of the log file. Empty means stdout only.
	Path string
	// Level: debug, info, warn, error. Empty means info.
	Level string
	// JSON switches the handler to JSON (recommended in production).
	JSON bool
	// MaxBytes and MaxFiles drive rotation (defaults: 1 MB × 10, which is what
	// the Ruby service this replaces used).
	MaxBytes int64
	MaxFiles int
	// AlsoStdout defaults to true.
	AlsoStdout bool
}

// New returns a logger and an io.Closer for the underlying file (nil when the
// logger writes to stdout only). Callers should close it on shutdown.
func New(opts Options) (*slog.Logger, io.Closer, error) {
	lvl, err := ParseLevel(opts.Level)
	if err != nil {
		return nil, nil, err
	}
	handlerOpts := &slog.HandlerOptions{Level: lvl}

	var writers []io.Writer
	var closer io.Closer

	if opts.Path != "" {
		maxBytes := opts.MaxBytes
		if maxBytes <= 0 {
			maxBytes = 1 << 20 // 1 MB
		}
		maxFiles := opts.MaxFiles
		if maxFiles <= 0 {
			maxFiles = 10
		}
		rw, err := NewRotatingWriter(opts.Path, maxBytes, maxFiles)
		if err != nil {
			return nil, nil, err
		}
		writers = append(writers, rw)
		closer = rw
	}

	// AlsoStdout is true unless explicitly disabled: the journal keeps working.
	if opts.Path == "" || opts.AlsoStdout {
		writers = append(writers, os.Stdout)
	}

	out := io.MultiWriter(writers...)
	if opts.JSON {
		return slog.New(slog.NewJSONHandler(out, handlerOpts)), closer, nil
	}
	return slog.New(slog.NewTextHandler(out, handlerOpts)), closer, nil
}

// ParseLevel maps a config string to a slog level. Unknown values are an error:
// silently logging less than you asked for is worse than not booting.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("logging: unknown level %q (use debug, info, warn or error)", s)
	}
}
