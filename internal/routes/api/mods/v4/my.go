// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// myCountMax caps one My page. Every explicit count in the captures renders
// the same maximum in the echo - the website's 12, the AOM client's 1000 and
// the AOE4 client's 20000 all answer 500 - while a body without count keeps
// the default 10.
const myCountMax = int64(500)

// My serves POST /api/v4/Mods/My/. The AOE3 client sends only {"game":3} and
// answers count 10; AOM sends count 1000 explicitly. The AOE3 My resolves the
// echo to the creator of the caller and the default count 10.
func My(deps shared.Deps) http.HandlerFunc {
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
		filter := request.ToFilter(deps.Pagination, shared.FilterDefaults{
			Sort:  "popular",
			Order: "desc",
			Count: 10,
		})
		if filter.Limit <= 0 || int64(filter.Limit) > myCountMax {
			filter.Limit = int(myCountMax)
		}
		filter.CreatorUserID = viewer.UserID

		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		// The echo carries the resolved filter: the creator of the caller
		// and 10, unless the body carried an explicit count - every such
		// capture echoes the 500 maximum, so that is what renders.
		echo := request
		if echo.Count.Set {
			// Any explicit count lands on the capped page of the captures.
			echo.Count.Value = myCountMax
		}
		echo = echo.WithEchoDefaults("popular", "desc", 10, &viewer.SteamID)
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(r, deps, echo.SearchParams(), page, viewer.LoggedOn))
	}
}
