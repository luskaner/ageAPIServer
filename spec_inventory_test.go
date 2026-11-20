package ageAPIServer

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// specDocument is the part of api_spec.yml the inventory contract reads.
type specDocument struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// normalizeSpecPath is the router.Normalize rule: lowercase and no trailing
// slash. The spec keeps its own capitalisation, the inventory does not.
func normalizeSpecPath(path string) string {
	return strings.TrimSuffix(strings.ToLower(path), "/")
}

// inventoryOnly would hold routes the server registers but the spec does not
// document. It is empty and meant to stay that way: even the local Ops
// endpoints (/test, /cacert.pem, /openapi.yml) are in api_spec.yml as implementation
// endpoints, so the bidirectional check below covers every route.
var inventoryOnly = map[string]bool{}

// TestOpenAPISpecIsServedFromTheBinary pins the embedded spec: the body is the
// file byte for byte, under the YAML content type.
func TestOpenAPISpecIsServedFromTheBinary(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodGet, "/openapi.yml", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/yaml; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	want, err := os.ReadFile("api_spec.yml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if recorder.Body.String() != string(want) {
		t.Fatal("the served spec differs from api_spec.yml")
	}
}

// TestSpecMatchesTheRouteInventory is the contract between routeInventory and
// api_spec.yml, in both directions: an endpoint cannot be served without being
// in the spec, and an operation cannot be in the spec without a route.
func TestSpecMatchesTheRouteInventory(t *testing.T) {
	data, err := os.ReadFile("api_spec.yml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc specDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	httpMethods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true,
		"patch": true, "head": true, "options": true,
	}
	spec := make(map[string]bool)
	for path, operations := range doc.Paths {
		for method := range operations {
			lower := strings.ToLower(method)
			if !httpMethods[lower] {
				continue
			}
			spec[strings.ToUpper(lower)+" "+normalizeSpecPath(path)] = true
		}
	}

	inventory := make(map[string]bool, len(routeInventory))
	for _, route := range routeInventory {
		key := route.Method + " " + normalizeSpecPath(route.SpecPath)
		inventory[key] = true
		if !spec[key] && !inventoryOnly[key] {
			t.Errorf("route %s is in routeInventory but not in api_spec.yml", key)
		}
	}
	for key := range spec {
		if !inventory[key] && !inventoryOnly[key] {
			t.Errorf("operation %s is in api_spec.yml but has no route in routeInventory", key)
		}
	}
}
