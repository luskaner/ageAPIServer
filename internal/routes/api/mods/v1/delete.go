// SPDX-License-Identifier: AGPL-3.0-only

package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// DeleteResponse is the body of the legacy Delete endpoint: the envelope the
// live capture carries, with "Deleted" as the resultKey and "true" as the
// resultValue.
type DeleteResponse struct {
	ErrorCode    int64   `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	ResultKey    string  `json:"resultKey"`
	ResultValue  string  `json:"resultValue"`
	ResultJSON   *string `json:"resultJson"`
}

// Delete serves POST /api/v1/Mods/Delete/. The body is {"id":N} and only the
// creator of the mod can delete it; the answer is always the envelope with
// the fixed key, even before the store decides the mod is gone. The v4
// variant answers the bare true instead.
func Delete(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.InstallRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		if err := deps.Stores.Mods.Delete(r.Context(), viewer.UserID, request.ID); err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, DeleteResponse{
			ErrorMessage: "0",
			ResultKey:    "Deleted",
			ResultValue:  "true",
		})
	}
}
