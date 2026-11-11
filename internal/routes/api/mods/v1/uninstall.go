package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// UninstallResponse is the body of the legacy Uninstall endpoint.
type UninstallResponse struct {
	ContentType        *string `json:"contentType"`
	SerializerSettings any     `json:"serializerSettings"`
	StatusCode         *int64  `json:"statusCode"`
	Value              bool    `json:"value"`
}

// Uninstall serves POST /api/v1/Mods/Uninstall/.
func Uninstall(deps shared.Deps) http.HandlerFunc {
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
		if err := deps.Stores.Mods.Uninstall(r.Context(), viewer.UserID, request.ID); err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		contentType := "application/json"
		status := int64(http.StatusOK)
		shared.WriteJSON(w, http.StatusOK, UninstallResponse{
			ContentType: &contentType,
			StatusCode:  &status,
			Value:       true,
		})
	}
}
