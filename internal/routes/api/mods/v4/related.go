package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Related serves GET /api/v4/mods/Related/{modId}.
func Related(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		viewer := router.ViewerFrom(r)
		page, err := deps.Stores.Mods.Related(r.Context(), modID, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		params := shared.SearchParams{Game: 0, Start: 1, Count: int64(len(page.Mods))}
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(params, page, viewer.LoggedOn))
	}
}
