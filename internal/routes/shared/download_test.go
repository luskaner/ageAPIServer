package shared

import (
	"net/http/httptest"
	"testing"
)

func TestBaseURLPlainHTTPDeployment(t *testing.T) {
	r := httptest.NewRequest("GET", "http://internal:8080/api/v4/mods/games", nil)
	if got := BaseURL(r, "", ""); got != "http://internal:8080" {
		t.Fatalf("base = %q", got)
	}
}

func TestBaseURLBehindAnExternalTerminator(t *testing.T) {
	// The proxy forwards the host but not X-Forwarded-Proto: proxy mode still
	// answers https, or the client would get an http download link.
	r := httptest.NewRequest("GET", "http://internal:8080/api/v4/mods/games", nil)
	if got := BaseURL(r, "", "https"); got != "https://internal:8080" {
		t.Fatalf("base = %q", got)
	}
}

func TestBaseURLRequestOverTLSWins(t *testing.T) {
	r := httptest.NewRequest("GET", "https://internal:8080/api/v4/mods/games", nil)
	if r.TLS == nil {
		t.Fatal("the test request is not a TLS one")
	}
	if got := BaseURL(r, "", "http"); got != "https://internal:8080" {
		t.Fatalf("base = %q", got)
	}
}

func TestBaseURLHonoursForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://internal:8080/api/v4/mods/games", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")

	if got := BaseURL(r, "", ""); got != "https://api.example.com" {
		t.Fatalf("base = %q", got)
	}
}

func TestBaseURLConfiguredWins(t *testing.T) {
	r := httptest.NewRequest("GET", "http://internal:8080/api/v4/mods/games", nil)
	if got := BaseURL(r, "https://cdn.example.com/", "http"); got != "https://cdn.example.com" {
		t.Fatalf("base = %q", got)
	}
}
