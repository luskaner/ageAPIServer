// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"net/http"
	"os"
)

// CACert serves the certificate the listener terminates TLS with, so an
// operator can install it on a client that has to trust this server without
// hunting for the file on disk.
//
// An empty path means the process does not terminate TLS itself (TLS mode off
// or proxy): there is no certificate of our own to hand out, so the route
// answers 404 instead of serving the pair a reverse proxy may use.
func CACert(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if path == "" {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		http.ServeFile(w, r, path)
	}
}
