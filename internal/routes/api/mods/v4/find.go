package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Find serves POST /api/v4/Mods/Find/. It is anonymous, but a request that
// carries credentials still gets the per-user fields filled in.
func Find(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.FindRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		filter := request.ToFilter(deps.Pagination, shared.FilterDefaults{Sort: "popular", Order: "desc"})
		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(r, deps, request.SearchParams(), page, viewer.LoggedOn))
	}
}
