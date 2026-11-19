// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Image serves GET /api/v4/mods/image/{name}: the screenshots Publish stored
// under UploadDir. It is anonymous because a thumbnail carries no user data and
// the game client may fetch it before it authenticates.
func Image(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Base turns any traversal the client sends into a plain name; the
		// stat below then only ever looks inside UploadDir.
		name := filepath.Base(strings.TrimSpace(r.PathValue("name")))
		if name == "" || name == "." || name == ".." || deps.UploadDir == "" {
			shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "image not found")
			return
		}
		path := filepath.Join(deps.UploadDir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "image not found")
			return
		}
		http.ServeFile(w, r, path)
	}
}
