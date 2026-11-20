// SPDX-License-Identifier: AGPL-3.0-only

package ageAPIServer

import (
	"net/http"
	"strings"
	"testing"
)

// routePath substitutes the path parameters with values the mux accepts.
func routePath(path string) string {
	path = strings.ReplaceAll(path, "{modid}", "1")
	path = strings.ReplaceAll(path, "{pollid}", "11111111-1111-1111-1111-111111111111")
	path = strings.ReplaceAll(path, "{name}", "missing.png")
	path = strings.ReplaceAll(path, "{matchid}", "1")
	path = strings.ReplaceAll(path, "{hash}", "missing")
	return path
}

// TestCORSAdvertisesTheInventoryMethods pins the header to the inventory, not
// to a literal: the PUT of CheckAndPublishFile has to be advertised for the
// browser preflight to let the upload through.
func TestCORSAdvertisesTheInventoryMethods(t *testing.T) {
	server, _ := newTestServer(t)
	want := strings.Join(inventoryMethods(server.Routes()), ", ")
	if !strings.Contains(want, "PUT") {
		t.Fatalf("inventory methods = %q, want PUT", want)
	}
	recorder := do(t, server.Handler(), http.MethodOptions, "/api/v4/mods/checkandpublishfile", "",
		map[string]string{
			"Origin":                        "https://www.ageofempires.com",
			"Access-Control-Request-Method": "PUT",
		})
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != want {
		t.Fatalf("allow-methods = %q, want %q", got, want)
	}
}

// TestAllRoutesAnswerPreflight checks that every registered route answers the
// browser preflight with 204. A POST-only pattern would answer 405 and the
// website could not call the API.
func TestAllRoutesAnswerPreflight(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()

	for _, route := range server.Routes() {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			recorder := do(t, handler, http.MethodOptions, routePath(route.Path), "",
				map[string]string{
					"Origin":                        "https://www.ageofempires.com",
					"Access-Control-Request-Method": route.Method,
				})
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("code = %d, want 204", recorder.Code)
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://www.ageofempires.com" {
				t.Fatalf("allow-origin = %q", got)
			}
		})
	}
}

// TestProtectedRoutesRejectAnonymous checks the 401 of every endpoint that
// declares authentication.
func TestProtectedRoutesRejectAnonymous(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()

	for _, route := range server.Routes() {
		if !route.AuthRequired {
			continue
		}
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			recorder := do(t, handler, route.Method, routePath(route.Path), "{}", nil)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401", recorder.Code)
			}
		})
	}
}

// TestAllRoutesAreRegistered checks that no route falls through to the 404
// handler or to a 405 from the mux.
func TestAllRoutesAreRegistered(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()
	auth := map[string]string{"Authorization": "Steam1.0 test-token"}

	for _, route := range server.Routes() {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			body := ""
			if route.Method == http.MethodPost {
				body = "{}"
			}
			recorder := do(t, handler, route.Method, routePath(route.Path), body, auth)
			switch recorder.Code {
			case http.StatusNotFound, http.StatusMethodNotAllowed:
				// A 404 is expected for the routes that address a resource that
				// does not exist; a 405 never is.
				if recorder.Code == http.StatusMethodNotAllowed {
					t.Fatalf("method not allowed: %s", recorder.Body.String())
				}
				if route.Path == "/test" || route.Path == "/api/v4/mods/games" ||
					route.Path == "/api/v4/mods/tags" || route.Path == "/poll/list" {
					t.Fatalf("unexpected 404: %s", recorder.Body.String())
				}
			}
		})
	}
}
