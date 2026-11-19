// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Unsubscribe serves GET /api/v4/Mods/Unsubscribe/{modId}. The schema declares
// UnsubscribeResponse as `type: boolean`, so the body is `true`, not an object.
func Unsubscribe(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		viewer := router.ViewerFrom(r)
		if err := deps.Stores.Mods.Unsubscribe(r.Context(), viewer.UserID, modID); err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, true)
	}
}
