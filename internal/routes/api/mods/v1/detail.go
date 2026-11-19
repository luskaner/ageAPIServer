// SPDX-License-Identifier: AGPL-3.0-only

package v1

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/models"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Detail serves POST /api/v1/Mods/Detail/. The capture sends only
// `{"modid":93963}`: `game` is optional in the spec and unused here.
func Detail(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.DetailRequestV1
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		detail, err := deps.Stores.Mods.Detail(r.Context(), request.ModID.Int(), router.ViewerFrom(r))
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.AbsolutizeDetail(r, deps, detail)
		shared.WriteJSON(w, http.StatusOK, models.LegacyDetailOf(*detail))
	}
}
