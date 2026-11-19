package ageAPIServer

import (
	_ "embed"
	"net/http"
)

// specYAML is the official API description. Embedding it keeps the handler
// independent of the working directory: the file already travels in every
// release, and a client generator can fetch it from the running binary.
//
//go:embed api_spec.yml
var specYAML []byte

// openAPISpec serves the embedded api_spec.yml. It is an Ops addition: the
// official service does not expose its own spec, so the route is not in
// api_spec.yml and the inventory test lists it as local-only.
func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(specYAML)
}
