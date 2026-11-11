package v1

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// InstallResponse is the body of the legacy Install endpoint: the
// SubscribeResponse wrapped in the serializer envelope the client expects.
type InstallResponse struct {
	ContentType        *string              `json:"contentType"`
	SerializerSettings any                  `json:"serializerSettings"`
	StatusCode         *int64               `json:"statusCode"`
	Value              *models.DownloadInfo `json:"value"`
}

// Install serves POST /api/v1/Mods/Install/.
func Install(deps shared.Deps) http.HandlerFunc {
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
		target, err := deps.Stores.Mods.Install(r.Context(), viewer.UserID, request.ID)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		info := shared.DownloadInfo(r, deps, target)
		contentType := "application/json"
		status := int64(http.StatusOK)
		shared.WriteJSON(w, http.StatusOK, InstallResponse{
			ContentType: &contentType,
			StatusCode:  &status,
			Value:       &info,
		})
	}
}
