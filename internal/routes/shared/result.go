package shared

import (
	"encoding/json"
	"errors"
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
)

// Error codes of the ErrorResponse body. They are local to this API: the
// upstream values are not observable from the captures.
const (
	CodeBadRequest   = 400
	CodeUnauthorized = 401
	CodeNotFound     = 404
	CodeInternal     = 500
)

// ErrorResponse is the body of every error.
type ErrorResponse struct {
	ErrorCode    int64  `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

// SearchParams is the echo of the request inside a ModList.
type SearchParams struct {
	Q       *string         `json:"q"`
	Creator *string         `json:"creator"`
	Filter  int64           `json:"filter"`
	Sort    *string         `json:"sort"`
	Order   *string         `json:"order"`
	Start   int64           `json:"start"`
	Count   int64           `json:"count"`
	Game    int64           `json:"game"`
	ModID   json.RawMessage `json:"modid"`
}

// ModList is the body of every list endpoint.
type ModList struct {
	SearchParams SearchParams `json:"searchParams"`
	ModList      []models.Mod `json:"modList"`
	TotalCount   *int64       `json:"totalCount"`
	IsLoggedOn   bool         `json:"isLoggedOn"`
	ErrorCode    int64        `json:"errorCode,omitempty"`
	ErrorMessage string       `json:"errorMessage,omitempty"`
}

// NewModList wraps a page of mods.
func NewModList(params SearchParams, page *models.ModPage, loggedOn bool) ModList {
	mods := page.Mods
	if mods == nil {
		mods = []models.Mod{}
	}
	total := page.TotalCount
	return ModList{
		SearchParams: params,
		ModList:      mods,
		TotalCount:   &total,
		IsLoggedOn:   loggedOn,
	}
}

// WriteJSON writes a JSON body with the given status.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
	i.JSON(w, body)
}

// WriteError writes an ErrorResponse.
func WriteError(w http.ResponseWriter, status int, code int64, message string) {
	WriteJSON(w, status, ErrorResponse{ErrorCode: code, ErrorMessage: message})
}

// WriteStoreError maps a store error to the matching status.
func WriteStoreError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case isNotFound(err):
		WriteError(w, http.StatusNotFound, CodeNotFound, "not found")
	case isUnauthorized(err):
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "unauthorized")
	case isValidation(err):
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error")
	}
}

func isNotFound(err error) bool     { return errors.Is(err, models.ErrNotFound) }
func isUnauthorized(err error) bool { return errors.Is(err, models.ErrUnauthorized) }
func isValidation(err error) bool   { return errors.Is(err, models.ErrValidation) }
