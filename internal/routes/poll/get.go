// SPDX-License-Identifier: AGPL-3.0-only

package poll

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Get serves GET /Poll/{pollId}. The poll id is a UUID, so it is matched in its
// canonical lowercase form; Normalize has already lowercased the path.
func Get(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pollID := r.PathValue("pollid")
		if pollID == "" {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid pollId")
			return
		}
		poll, err := deps.Stores.Polls.Get(r.Context(), pollID, router.ViewerFrom(r))
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, poll)
	}
}
