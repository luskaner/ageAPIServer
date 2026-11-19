package v4

import (
	"net/http"
	"strconv"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// PublishResponse is the body of Publish. The upstream answers errorCode 0 and
// puts the new mod id in resultValue, as a string.
type PublishResponse struct {
	ErrorCode    int64   `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	ResultKey    string  `json:"resultKey"`
	ResultValue  string  `json:"resultValue"`
	ResultJSON   *string `json:"resultJson"`
}

// Publish serves POST /api/v4/mods/Publish. The body is multipart/form-data:
// the scalar fields describe the mod, modFiles carries the archive and
// modImages the screenshots, matched to ImageMetaData by file name.
func Publish(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.ParsePublish(r)
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}
		if err := form.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}

		viewer := router.ViewerFrom(r)
		input := models.PublishInput{
			ModID:         form.ModID,
			GameTitleID:   form.GameTitleID,
			ModName:       form.ModName,
			Description:   form.Description,
			ChangeList:    form.ChangeList,
			ModVisibility: form.ModVisibility,
			ModTypeID:     form.ModTypeID,
			ModDiscussion: form.ModDiscussion,
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
			// The AOM capture answers the uncompressed total of its zip in
			// modDiskSize (6654 versus the 2813 compressed), so the size of
			// the archive entries rides along whenever a file is present.
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
		shared.WriteJSON(w, http.StatusOK, PublishResponse{
			ErrorMessage: "0",
			ResultKey:    "ModId",
			ResultValue:  strconv.FormatInt(modID, 10),
		})
	}
}
