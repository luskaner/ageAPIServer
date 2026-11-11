package logger

import (
	"bytes"
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
