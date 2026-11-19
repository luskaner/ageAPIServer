// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Subscribe serves GET /api/v4/Mods/Subscribe/{modId}. It records the
// subscription and does not touch the download counter: the AOE4 client calls
// Subscribe and Download for the same mod.
func Subscribe(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		viewer := router.ViewerFrom(r)
		target, err := deps.Stores.Mods.Subscribe(r.Context(), viewer.UserID, modID)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, shared.DownloadInfo(r, deps, target))
	}
}
