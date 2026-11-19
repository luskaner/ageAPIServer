// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Download serves GET /api/v4/Mods/Download/{modId}/. It counts the download
// once per user, so a retry does not inflate the counter.
func Download(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		viewer := router.ViewerFrom(r)
		target, err := deps.Stores.Mods.Download(r.Context(), viewer.UserID, modID)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, shared.DownloadInfo(r, deps, target))
	}
}
