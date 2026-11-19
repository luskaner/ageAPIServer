package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Featured serves POST /api/v4/mods/Featured. The AOM client sends only
// `{"game":1001}` and the echo resolves to sort popular and count 10; the
// website sends its sort, count and filters explicitly and echoes them as
// sent. The rows carry the featured status 5, which the store filter picks.
func Featured(deps shared.Deps) http.HandlerFunc {
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
		filter.FeaturedOnly = true

		page, err := deps.Stores.Mods.Find(r.Context(), filter, viewer)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		// The echo carries the resolved filter: the AOM capture resolves
		// sort popular and count 10 for a body that only carried the game,
		// with the creator null (only Installed answers "" and only My the
		// caller).
		echo := request.WithEchoDefaults("popular", "desc", 10, nil)
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(r, deps, echo.SearchParams(), page, viewer.LoggedOn))
	}
}
