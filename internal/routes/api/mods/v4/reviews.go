package v4

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// ReviewsResponse is the body of Reviews. The capture carries totalCount as
// null when the mod has no reviews, so it is a pointer.
type ReviewsResponse struct {
	ContinuationToken *string         `json:"continuationToken"`
	Reviews           []models.Review `json:"reviews"`
	TotalCount        *int64          `json:"totalCount"`
}

// Reviews serves POST /api/v4/mods/Reviews. `ratingNum` is passed through
// without validating it against 1..5: the capture sends 7.
func Reviews(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.ReviewsRequest
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
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid modId")
			return
		}

		page := int(request.Page.OrDefault(1))
		count := int(request.Count.OrDefault(5))
		reviews, total, err := deps.Stores.Mods.Reviews(r.Context(), modID, page, count, request.RatingNum.Value)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		response := ReviewsResponse{Reviews: reviews}
		if total > 0 {
			response.TotalCount = &total
		}
		shared.WriteJSON(w, http.StatusOK, response)
	}
}
