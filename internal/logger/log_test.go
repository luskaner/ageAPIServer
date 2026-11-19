// SPDX-License-Identifier: AGPL-3.0-only

package logger

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactHeaderKeepsScheme(t *testing.T) {
	got := RedactHeader("Authorization", "Steam1.0 c2VjcmV0LXRva2Vu")
	if got != "Steam1.0 "+Redacted {
		t.Fatalf("unexpected redaction: %q", got)
	}
	if strings.Contains(got, "c2VjcmV0") {
		t.Fatal("token leaked")
	}
}

func TestRedactHeaderLeavesOrdinaryHeaders(t *testing.T) {
	if got := RedactHeader("Content-Type", "application/json"); got != "application/json" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestRedactCookieDropsValues(t *testing.T) {
	got := RedactCookie(".AgeOfEmpiresServices=abc123; other=def")
	if strings.Contains(got, "abc123") || strings.Contains(got, "def") {
		t.Fatalf("cookie value leaked: %q", got)
	}
	if !strings.Contains(got, ".AgeOfEmpiresServices=") {
		t.Fatalf("cookie name lost: %q", got)
	}
}

func TestRedactHeadersCoversCookies(t *testing.T) {
	headers := map[string][]string{
		"Cookie":        {".AgeOfEmpiresServices=secret"},
		"Authorization": {"Steam1.0 secret"},
	}
	redacted := RedactHeaders(headers)
	for name, value := range redacted {
		if strings.Contains(value, "secret") {
			t.Fatalf("%s leaked: %q", name, value)
		}
	}
}

func TestCommLogNeverPrintsCredentials(t *testing.T) {
	var buffer bytes.Buffer
	Initialize(&buffer)
	t.Cleanup(func() { Initialize(nil) })

	CommLog("req-1", "POST", "/api/v4/mods/find", 200, 0)
	if !strings.Contains(buffer.String(), "req-1") {
		t.Fatalf("request id missing: %q", buffer.String())
	}
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

// TestFileLogHonoursTheLevel pins the per-line level: at error the file keeps
// the failure and drops the informational line.
func TestFileLogHonoursTheLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	Initialize(io.Discard)
	t.Cleanup(func() { Close(); Initialize(nil) })

	if err := OpenMainFileLog(path, true, "error", 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	Info("informational line")
	Warn("warning line")
	Fail("failure line")
	Close()

	content := readLogFile(t, path)
	if !strings.Contains(content, "failure line") {
		t.Fatalf("failure missing: %q", content)
	}
	if strings.Contains(content, "informational line") {
		t.Fatalf("info line written at error level: %q", content)
	}
	if strings.Contains(content, "warning line") {
		t.Fatalf("warn line written at error level: %q", content)
	}
}

func TestFileLogKeepsDebugLinesAtDebugLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	Initialize(io.Discard)
	t.Cleanup(func() { Close(); Initialize(nil) })

	if err := OpenMainFileLog(path, true, "debug", 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	Debug("diagnostic line")
	Close()

	if content := readLogFile(t, path); !strings.Contains(content, "diagnostic line") {
		t.Fatalf("debug line missing: %q", content)
	}
}

// TestConsoleShowsEveryLevel pins that the configured level only filters the
// file: the console output is identical at error and at debug.
func TestConsoleShowsEveryLevel(t *testing.T) {
	run := func(level string) string {
		var buffer bytes.Buffer
		Initialize(&buffer)
		if err := OpenMainFileLog(filepath.Join(t.TempDir(), "app.log"), true, level, 0); err != nil {
			t.Fatalf("open: %v", err)
		}
		Info("info line")
		Debug("debug line")
		Warn("warn line")
		Fail("fail line")
		Close()
		return buffer.String()
	}
	t.Cleanup(func() { Close(); Initialize(nil) })

	atError := run("error")
	atDebug := run("debug")
	if atError != atDebug {
		t.Fatalf("console differs by level:\nerror:\n%s\ndebug:\n%s", atError, atDebug)
	}
	for _, want := range []string{"info line", "debug line", "warn line", "fail line"} {
		if !strings.Contains(atError, want) {
			t.Fatalf("console is missing %q: %q", want, atError)
		}
	}
}

func TestStdLoggerWritesWarnings(t *testing.T) {
	var buffer bytes.Buffer
	Initialize(&buffer)
	t.Cleanup(func() { Close(); Initialize(nil) })

	if _, err := StdLogger().Writer().Write([]byte("http: TLS handshake error from 127.0.0.1\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := buffer.String(); !strings.Contains(got, "[HTTP] http: TLS handshake error") {
		t.Fatalf("line = %q", got)
	}
}

func TestOpenMainFileLogRotatesWhenTooBig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	Initialize(io.Discard)
	t.Cleanup(func() { Close(); Initialize(nil) })

	if err := OpenMainFileLog(path, true, "info", 50); err != nil {
		t.Fatalf("open: %v", err)
	}
	Close()

	if content := readLogFile(t, path); strings.Contains(content, "xxxx") {
		t.Fatalf("the new file still carries the old content: %q", content)
	}
	old := readLogFile(t, path+".old")
	if !strings.Contains(old, strings.Repeat("x", 100)) {
		t.Fatalf("the old generation is missing: %q", old)
	}
}

func TestOpenMainFileLogWithoutALimitDoesNotRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	Initialize(io.Discard)
	t.Cleanup(func() { Close(); Initialize(nil) })

	if err := OpenMainFileLog(path, true, "info", 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	Close()
	if _, err := os.Stat(path + ".old"); !os.IsNotExist(err) {
		t.Fatalf("a rotation happened without a limit: %v", err)
	}
	if content := readLogFile(t, path); !strings.Contains(content, strings.Repeat("x", 100)) {
		t.Fatal("the existing file was truncated")
	}
}

func TestDisabledFileLogCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	Initialize(io.Discard)
	t.Cleanup(func() { Close(); Initialize(nil) })
	if err := OpenMainFileLog(filepath.Join(dir, "app.log"), false, "info", 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	Info("nothing is written")
	Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("disabled logger created %d files", len(entries))
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]string{
		"debug": "DEBUG",
		"info":  "INFO",
		"warn":  "WARN",
		"error": "ERROR",
		"loud":  "INFO",
		"":      "INFO",
	}
	for name, want := range cases {
		if got := ParseLevel(name).String(); got != want {
			t.Fatalf("%q: got %s, want %s", name, got, want)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	Initialize(io.Discard)
	t.Cleanup(func() { Initialize(nil) })
	Close()
	Close()
}
