package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Installed serves POST /api/v1/Mods/Installed/. The AOE2 client sends
// `{"game":2,"q":"*"}`, so `*` must be read as "no filter".
func Installed(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.FindRequestV1
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		filter := request.ToFilter(deps.Pagination, shared.FilterDefaults{
			Sort:  "popular",
			Order: "desc",
			Count: 45,
		})
		filter.InstalledUserID = viewer.UserID

		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, shared.NewLegacyModList(r, deps, request.SearchParams(), page, viewer.LoggedOn, true))
	}
}
