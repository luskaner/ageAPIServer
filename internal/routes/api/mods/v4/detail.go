package v4

import (
	"errors"
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Detail serves GET /api/v4/Mods/Detail/{modId}.
func Detail(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID, err := modIDFromPath(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}
		detail, err := deps.Stores.Mods.Detail(r.Context(), modID, router.ViewerFrom(r))
		if err != nil {
			if errors.Is(err, models.ErrNotFound) {
				// The Detail of a deleted mod does not carry the mod error
				// envelope: it answers the RFC 9110 ProblemDetails body of the
				// web server, as the capture of the deleted mod shows.
				shared.WriteProblemDetails(w, http.StatusNotFound)
				return
			}
			shared.WriteStoreError(w, err)
			return
		}
		shared.AbsolutizeDetail(r, deps, detail)
		// The captures answer some unset fields with fixed defaults instead of
		// null: a mod without a version renders 1.1, and the verified version
		// and the status message render as empty strings.
		if detail.ModVersion == 0 {
			detail.ModVersion = 1.1
		}
		if detail.VerifiedVersion == nil {
			detail.VerifiedVersion = ptr("")
		}
		if detail.ModStatusMessage == nil {
			detail.ModStatusMessage = ptr("")
		}
		shared.WriteJSON(w, http.StatusOK, detail)
	}
}

// ptr is a shorthand string pointer for the fixed defaults of the capture.
func ptr(value string) *string {
	return &value
}
