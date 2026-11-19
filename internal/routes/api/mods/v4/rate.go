// SPDX-License-Identifier: AGPL-3.0-only

package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// RateResponse is the body of Rate. The capture carries statusCode and
// responseMessage as null, so they stay nil.
type RateResponse struct {
	StatusCode      *int64          `json:"statusCode"`
	ResponseMessage *string         `json:"responseMessage"`
	PlayFabResponse *models.Ratings `json:"playFabResponse"`
}

// Rate serves POST /api/v4/Mods/Rate/. The aggregates of the mod are refreshed
// in the same transaction, so a later sort by rating is not stale.
func Rate(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.RateRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		ratings, err := deps.Stores.Ratings.Rate(r.Context(), viewer.UserID, request.ModID, request.Rating)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}

		shared.WriteJSON(w, http.StatusOK, RateResponse{PlayFabResponse: ratings})
	}
}
