// Package logger is the process logger: a prefixed console writer plus an
// optional file, both on top of log/slog. It also owns the single redaction
// point for credentials, so no other package can leak a Steam token or a
// session cookie into a log line.
package logger

import (
	"fmt"
	"io"
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
// no-op, so the caller does not have to branch.
func OpenMainFileLog(path string, enabled bool, levelName string) error {
	mu.Lock()
	defer mu.Unlock()
	level = ParseLevel(levelName)
	if !enabled || path == "" {
		return nil
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

func write(prefix string, format string, a ...any) {
	message := fmt.Sprintf(format, a...)
	mu.Lock()
	defer mu.Unlock()
	_, _ = fmt.Fprintf(console, "%s %s\n", prefix, message)
	if fileLogger != nil {
		fileLogger.Info(message, "prefix", prefix)
	}
}

// Ok, Fail, Warn, Info, Step, Detail and Fault are the styled console verbs the
// rest of the module speaks to.
func Ok(format string, a ...any)     { write("[ OK ]", format, a...) }
func Fail(format string, a ...any)   { write("[FAIL]", format, a...) }
func Warn(format string, a ...any)   { write("[WARN]", format, a...) }
func Info(format string, a ...any)   { write("[INFO]", format, a...) }
func Step(format string, a ...any)   { write("[STEP]", format, a...) }
func Detail(format string, a ...any) { write("[ .. ]", format, a...) }
func Fault(format string, a ...any)  { write("[ERR ]", format, a...) }

// Debugf logs at debug level, to the file only.
func Debugf(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	if fileLogger != nil && level <= slog.LevelDebug {
		fileLogger.Debug(fmt.Sprintf(format, a...))
	}
}

// Banner prints the one-line program header.
func Banner(version string) {
	write("[====]", "ageAPIServer %s (started %s)", version, StartTime.Format(time.RFC3339))
}

// Section prints a phase header.
func Section(name string) {
	write("[----]", "%s", name)
}

// KV prints an indented key/value pair.
func KV(indent int, key string, value string) {
	write("[ .. ]", "%s%s: %s", strings.Repeat("  ", indent), key, value)
}
