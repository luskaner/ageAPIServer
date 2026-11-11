package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// FlagResponse is the body of the legacy Flag endpoint.
type FlagResponse struct {
	ErrorCode    int64  `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	ResultKey    string `json:"resultKey"`
	ResultValue  string `json:"resultValue"`
	ResultJSON   any    `json:"resultJson"`
}

// Flag serves POST /api/v1/Mods/Flag/.
func Flag(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.FlagRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		if err := deps.Stores.Mods.Flag(r.Context(), viewer.UserID, request.ID, request.StringValue, request.BoolValue); err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, FlagResponse{
			ResultKey:   "flag",
			ResultValue: request.StringValue,
		})
	}
}
