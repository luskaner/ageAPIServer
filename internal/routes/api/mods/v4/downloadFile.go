// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// DownloadFile serves GET /api/v4/Mods/Download/{modId}/file, which is the
// target of the downloadUrl that Subscribe and Download hand to the client. It
// does not count a download: the counter belongs to the JSON endpoint.
func DownloadFile(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		target, err := deps.Stores.Mods.DownloadTargetFor(r.Context(), modID)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		if target.FileURL == "" {
			shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "mod has no file")
			return
		}
		if deps.UploadDir == "" {
			shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, "no upload directory configured")
			return
		}
		// The stored fileUrl is the name SaveUpload chose inside UploadDir.
		// Base drops any directory the column could carry, so a crafted value
		// cannot escape the upload folder.
		name := filepath.Base(target.FileURL)
		path := filepath.Join(deps.UploadDir, name)
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "file not found")
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeFile(w, r, path)
	}
}
