// SPDX-License-Identifier: AGPL-3.0-only

// Package logger is the process logger: a prefixed console writer plus an
// optional file, both on top of log/slog. It also owns the single redaction
// point for credentials, so no other package can leak a Steam token or a
// session cookie into a log line.
package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// StartTime is the process start, used by the banner and the file header.
var StartTime = time.Now().UTC()

var (
	mu         sync.Mutex
	console    io.Writer = os.Stdout
	fileHandle *os.File
	fileLogger *slog.Logger
	level      = slog.LevelInfo
)

// Initialize points the console writer at w. Tests pass a buffer.
func Initialize(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		w = io.Discard
	}
	console = w
}

// ParseLevel maps a config value to a slog level.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// OpenMainFileLog opens the log file. An empty path or a disabled logger is a
// no-op, so the caller does not have to branch. maxSizeBytes rotates the
// previous file to <path>.old when it is already bigger: one generation, checked
// only at startup, which is the whole rotation this process performs.
func OpenMainFileLog(path string, enabled bool, levelName string, maxSizeBytes int64) error {
	mu.Lock()
	defer mu.Unlock()
	level = ParseLevel(levelName)
	if !enabled || path == "" {
		return nil
	}
	if maxSizeBytes > 0 {
		if info, err := os.Stat(path); err == nil && info.Size() > maxSizeBytes {
			// os.Rename does not replace an existing file on Windows, so the
			// previous generation is removed first.
			_ = os.Remove(path + ".old")
			if err := os.Rename(path, path+".old"); err != nil {
				return fmt.Errorf("rotate log file: %w", err)
			}
		}
	}
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fileHandle = handle
	fileLogger = slog.New(slog.NewTextHandler(handle, &slog.HandlerOptions{Level: level}))
	return nil
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if fileHandle != nil {
		_ = fileHandle.Close()
		fileHandle = nil
		fileLogger = nil
	}
}

// write renders one line to the console and, when the file logger is open, to
// the file at the line's own level. The console keeps printing every line: the
// configured level is the minimum the file records.
func write(level slog.Level, prefix string, format string, a ...any) {
	message := fmt.Sprintf(format, a...)
	mu.Lock()
	defer mu.Unlock()
	_, _ = fmt.Fprintf(console, "%s %s\n", prefix, message)
	if fileLogger != nil {
		fileLogger.Log(context.Background(), level, message, "prefix", prefix)
	}
}

// Ok writes the styled console line of a success; Fail, Warn, Info, Detail and
// Fault are the same shape for the other verbs. Ok, Info, Detail, Section, KV
// and Banner are informational; Warn is a warning and Fail and Fault are
// errors, which is the level the file logger records them at.
func Ok(format string, a ...any)     { write(slog.LevelInfo, "[ OK ]", format, a...) }
func Fail(format string, a ...any)   { write(slog.LevelError, "[FAIL]", format, a...) }
func Warn(format string, a ...any)   { write(slog.LevelWarn, "[WARN]", format, a...) }
func Info(format string, a ...any)   { write(slog.LevelInfo, "[INFO]", format, a...) }
func Detail(format string, a ...any) { write(slog.LevelInfo, "[ .. ]", format, a...) }
func Fault(format string, a ...any)  { write(slog.LevelError, "[ERR ]", format, a...) }

// Debug is the level for lines that only matter while diagnosing a problem; it
// is written to the file when the configured level is debug and printed to the
// console like every other line.
func Debug(format string, a ...any) { write(slog.LevelDebug, "[ .. ]", format, a...) }

// Banner prints the one-line program header.
func Banner(version string) {
	write(slog.LevelInfo, "[====]", "ageAPIServer %s (started %s)", version, StartTime.Format(time.RFC3339))
}

// Section prints a phase header.
func Section(name string) {
	write(slog.LevelInfo, "[----]", "%s", name)
}

// KV prints an indented key/value pair.
func KV(indent int, key string, value string) {
	write(slog.LevelInfo, "[ .. ]", "%s%s: %s", strings.Repeat("  ", indent), key, value)
}

// StdLogger adapts the process logger to *log.Logger for http.Server.ErrorLog:
// net/http handshake errors belong in the log file, not in raw stderr.
func StdLogger() *log.Logger {
	return log.New(&stdWriter{}, "", 0)
}

// stdWriter is the bridge from the standard log package into the leveled
// writer above. The lines net/http emits carry no headers, so no credential can
// reach this path; it goes through the same pipeline for consistency.
type stdWriter struct{}

func (stdWriter) Write(p []byte) (int, error) {
	if line := strings.TrimSpace(string(p)); line != "" {
		write(slog.LevelWarn, "[HTTP]", "%s", line)
	}
	return len(p), nil
}
