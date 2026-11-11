package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeRewritesThePath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"//api//v4//mods//games//", "/api/v4/mods/games"},
		{"/API/V4/MODS/GAMES", "/api/v4/mods/games"},
		{"/api/v4/Mods/Detail/583126/", "/api/v4/mods/detail/583126"},
		{"/api/v4/mods/UnSubscribe/890", "/api/v4/mods/unsubscribe/890"},
		{"/api/v4/mods/Find/", "/api/v4/mods/find"},
		{"/", "/"},
		{"", "/"},
		{"/./api/v4/mods/games", "/api/v4/mods/games"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			var seen string
			handler := Normalize(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = r.URL.Path
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.URL.Path = tc.path
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if seen != tc.want {
				t.Fatalf("got %q, want %q", seen, tc.want)
			}
		})
	}
}

func TestNormalizeRejectsEscapingPaths(t *testing.T) {
	handler := Normalize(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be reached")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.URL.Path = "/../etc/passwd"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", recorder.Code)
	}
}

func TestNormalizeRejectsEncodedSlash(t *testing.T) {
	handler := Normalize(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be reached")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.URL.Path = "/api/v4/mods/detail/1"
	request.URL.RawPath = "/api/v4/mods/detail/1%2F2"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", recorder.Code)
	}
}
