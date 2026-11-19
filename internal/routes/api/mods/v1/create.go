package v1

import (
	"net/http"
	"strconv"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// CreateResponse is the body of the legacy Create endpoint: the same envelope
// the live carry carries, with the new mod id in resultValue and "ModId" as
// the resultKey.
type CreateResponse struct {
	ErrorCode    int64   `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	ResultKey    string  `json:"resultKey"`
	ResultValue  string  `json:"resultValue"`
	ResultJSON   *string `json:"resultJson"`
}

// Create serves POST /api/v1/Mods/Create/. The body is multipart/form-data
// with the screenshots (modImages, paired to ImageMetaData by file name), the
// archive (modFiles) and the scalar fields of the form; the answer is the
// envelope with the id of the created mod.
func Create(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.ParseCreate(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}
		if err := form.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		// ModId of an owned mod updates it in place - the AOE2 client
		// re-publishes through this endpoint after the upload, so "Create"
		// answers the same id, and the 0 of a real create allocates one.
		// Anything else (foreign or missing mod) is the store's error.
		input := models.PublishInput{
			ModID:         form.ModID,
			GameTitleID:   form.GameTitleID,
			ModName:       form.ModName,
			Description:   form.Description,
			ChangeList:    form.ChangeList,
			ModVisibility: form.ModVisibility,
			ModTypeID:     form.ModTypeID,
			TagIDs:        form.TagIDs,
			CreatorID:     viewer.UserID,
		}

		if form.File != nil {
			saved, err := shared.SaveUpload(deps.UploadDir, form.File)
			if err != nil {
				shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, err.Error())
				return
			}
			input.FileURL = saved.Path
			input.FileCRC = saved.CRC
			input.FileSize = saved.Size
			input.DiskSize = shared.DiskSize(form.File)
		}
		images, err := shared.PublishImages(deps.UploadDir, form)
		if err != nil {
			shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, err.Error())
			return
		}
		input.Images = images

		modID, err := deps.Stores.Mods.Publish(r.Context(), input)
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, CreateResponse{
			ErrorMessage: "0",
			ResultKey:    "ModId",
			ResultValue:  strconv.FormatInt(modID, 10),
		})
	}
}
