package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Games serves GET /api/v4/mods/Games. It is anonymous: the spec declares
// `security: []`.
func Games(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		games, err := deps.Stores.Mods.Games(r.Context())
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, games)
	}
}
