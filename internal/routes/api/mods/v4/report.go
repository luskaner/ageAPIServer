package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Report serves POST /api/v4/mods/Report. The body carries the mod id and the
// concern as strings, and the answer is the bare JSON literal true.
func Report(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.ReportRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}
		modID, err := models.ParseModID(request.ModID)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "modId is required")
			return
		}

		viewer := router.ViewerFrom(r)
		err = deps.Stores.Mods.Report(r.Context(), viewer.UserID, models.ReportInput{
			ModID:               modID,
			ConcernCategoryID:   request.ConcernCategoryID,
			ConcernCategoryName: request.ConcernCategoryName,
			ConcernContent:      request.ConcernContent,
		})
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, true)
	}
}
