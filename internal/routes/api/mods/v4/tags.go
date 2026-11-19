// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"
	"strconv"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Tags serves GET /api/v4/mods/Tags. The optional `game` query parameter
// narrows the catalog; without it every game's tags are returned.
func Tags(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var gameTitleID int64
		if raw := r.URL.Query().Get("game"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "game must be an integer")
				return
			}
			gameTitleID = parsed
		}
		tags, err := deps.Stores.Mods.Tags(r.Context(), gameTitleID)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, tags)
	}
}
