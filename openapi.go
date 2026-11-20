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

// openAPISpec serves the embedded api_spec.yml. It is a local Ops endpoint:
// the upstream service never publishes its own spec, and api_spec.yml
// documents the route (along with /test and /cacert.pem) as an implementation endpoint,
// which the inventory test cross-checks like any other.
func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(specYAML)
}
