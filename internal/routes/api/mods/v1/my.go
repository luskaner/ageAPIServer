// SPDX-License-Identifier: AGPL-3.0-only

package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// My serves POST /api/v1/Mods/My/.
func My(deps shared.Deps) http.HandlerFunc {
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
		filter.CreatorUserID = viewer.UserID

		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		response := shared.NewLegacyModList(r, deps, request.SearchParams(), page, viewer.LoggedOn, false)
		shared.WriteJSON(w, http.StatusOK, response)
	}
}
