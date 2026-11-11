package v4

import (
	"mime/multipart"
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
		}

		images, err := publishImages(deps.UploadDir, form)
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

// publishImages stores the uploaded screenshots and pairs them with their
// metadata. An entry without a matching upload keeps the thumbnail the client
// sent, which is how an update reuses an image that already exists.
func publishImages(dir string, form shared.PublishForm) ([]models.PublishImage, error) {
	if len(form.ImageMeta) == 0 {
		return nil, nil
	}
	uploads := make(map[string]*multipart.FileHeader, len(form.Images))
	for _, header := range form.Images {
		uploads[header.Filename] = header
	}

	images := make([]models.PublishImage, 0, len(form.ImageMeta))
	for _, meta := range form.ImageMeta {
		image := models.PublishImage{
			DisplayIndex: meta.DisplayIndex,
			ImageName:    meta.ImageName,
			Thumbnail:    meta.ImageThumbnail,
		}
		if header, ok := uploads[meta.ImageName]; ok {
			saved, err := shared.SaveUpload(dir, header)
			if err != nil {
				return nil, err
			}
			image.Thumbnail = saved.Path
			image.FileSize = saved.Size
		}
		images = append(images, image)
	}
	return images, nil
}
