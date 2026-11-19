// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Installed serves POST /api/v4/Mods/Installed/. AOE3 and AOM send only
// `{"game":N}`, so the filter defaults must be applied server side: the echo
// resolves them as sort popular, order DESC and count 1000.
func Installed(deps shared.Deps) http.HandlerFunc {
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
			Count: 1000,
		})
		filter.InstalledUserID = viewer.UserID

		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		// The echo of the capture resolves the defaults: creator "" (not the
		// caller) and count 1000 for a request that only carried the game.
		empty := ""
		echo := request.WithEchoDefaults("popular", "desc", 1000, &empty)
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(r, deps, echo.SearchParams(), page, viewer.LoggedOn))
	}
}
