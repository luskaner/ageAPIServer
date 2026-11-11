package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func corsHandler() http.Handler {
	return CORS(CORSConfig{
		AllowedOrigins:   []string{"https://www.ageofempires.com"},
		AllowCredentials: true,
		MaxAge:           600 * time.Second,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestCORSPreflightIsAnsweredBeforeTheMux(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/v4/mods/find", nil)
	request.Header.Set("Origin", "https://www.ageofempires.com")
	request.Header.Set("Access-Control-Request-Method", "POST")
	recorder := httptest.NewRecorder()

	corsHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://www.ageofempires.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("max-age = %q", got)
	}
}

func TestCORSRejectsUnknownOriginWithoutWildcard(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/v4/mods/find", nil)
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()

	corsHandler().ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q, want empty", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow-credentials = %q, want empty", got)
	}
}

func TestCORSAddsHeadersToTheRealResponse(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v4/mods/games", nil)
	request.Header.Set("Origin", "https://www.ageofempires.com")
	recorder := httptest.NewRecorder()

	corsHandler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://www.ageofempires.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q", got)
	}
}
