package v4

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/luskaner/ageAPIServer/internal/models"
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
		switch {
		case errors.Is(err, models.ErrNotFound):
			// The capture of a deleted mod answers 204: the endpoint has no
			// related content to offer rather than an error body.
			w.WriteHeader(http.StatusNoContent)
			return
		case err != nil:
			shared.WriteStoreError(w, err)
			return
		}
		// The capture echoes a fixed page of six with start 0 and the mod id as
		// a string, unlike Find, which echoes the request it received. Its
		// totalCount is the size of that page, not the number of candidates.
		page.TotalCount = int64(len(page.Mods))
		if len(page.Mods) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// The capture carries modTags null in the Related rows, unlike Find.
		for i := range page.Mods {
			page.Mods[i].ModTags = nil
		}
		params := shared.SearchParams{
			Start: 0,
			Count: models.RelatedPageSize,
			ModID: json.RawMessage(strconv.Quote(strconv.FormatInt(modID, 10))),
		}
		shared.WriteJSON(w, http.StatusOK, shared.NewModList(r, deps, params, page, viewer.LoggedOn))
	}
}
