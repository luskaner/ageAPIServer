// SPDX-License-Identifier: AGPL-3.0-only

package matchcontent

import (
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/logger"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// multipartSlack is added to the per-file limit before it is applied to the raw
// request body, so the multipart framing (boundaries and part headers) never
// trips the reader before the counting writer enforces the exact file size.
const multipartSlack = 1 << 20

// Upload implements POST /api/MatchContent/. The multipart body carries the
// raw savegame in the "fileContent" part and the advertisement id in the
// "matchId" part.
//
// AoE2 DE parses the response as JSON and requires the boolean isError field.
func Upload(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files := deps.MatchContent
		if files == nil || !files.Enabled() {
			shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "not found")
			return
		}
		maxFileSize := files.MaxFileSize()
		if maxFileSize > 0 && maxFileSize < math.MaxInt64-multipartSlack {
			r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+multipartSlack)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid multipart body")
			return
		}
		var stage *models.CustomFileStage
		var matchId int32
		var haveMatchId bool
		var filename string
		var contentType string
		defer func() { files.Discard(stage) }()
		for {
			part, partErr := mr.NextPart()
			if errors.Is(partErr, io.EOF) {
				break
			}
			if partErr != nil {
				if tooLarge(partErr) {
					shared.WriteError(w, http.StatusRequestEntityTooLarge, shared.CodeBadRequest, "content too large")
					return
				}
				shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid multipart body")
				return
			}
			switch part.FormName() {
			case "fileContent":
				if stage != nil {
					_ = part.Close()
					shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "duplicate fileContent")
					return
				}
				filename = part.FileName()
				contentType = part.Header.Get("Content-Type")
				stage, err = files.Stage(func(dst io.Writer) error {
					_, copyErr := io.Copy(dst, part)
					return copyErr
				})
				if err != nil {
					if tooLarge(err) {
						shared.WriteError(w, http.StatusRequestEntityTooLarge, shared.CodeBadRequest, "content too large")
						return
					}
					logger.Warn("Failed to stage match file upload: %s", err)
					shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, "cannot store the file")
					return
				}
			case "matchId":
				value, readErr := io.ReadAll(io.LimitReader(part, 64))
				if readErr != nil {
					shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid matchId")
					return
				}
				id, parseErr := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 32)
				if parseErr != nil || id <= 0 {
					shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid matchId")
					return
				}
				matchId = int32(id)
				haveMatchId = true
			default:
				_, _ = io.Copy(io.Discard, part)
			}
			_ = part.Close()
		}
		if stage == nil || !haveMatchId || stage.Length() <= 0 {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "incomplete upload")
			return
		}
		if !models.KnownCustomFileExtension(filename) {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "unsupported file type")
			return
		}
		cf, err := files.Adopt(stage, matchId, filename, contentType)
		if err != nil {
			logger.Warn("Failed to store match file %d: %s", matchId, err)
			shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, "cannot store the file")
			return
		}
		logger.Info("Stored match file %d (%s, %d bytes)", cf.Id, cf.Filename, cf.Length)
		shared.WriteJSON(w, http.StatusOK, struct {
			IsError bool `json:"isError"`
		}{})
	}
}

// tooLarge reports whether err is the upload being rejected on size. Either the
// counting writer in the store or the raw body reader can be the one to trip,
// depending on how much multipart framing reaches the reader before the file
// does, and both mean the same thing to the client.
func tooLarge(err error) bool {
	if errors.Is(err, models.ErrFileTooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
