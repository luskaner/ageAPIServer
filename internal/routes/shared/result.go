package shared

import (
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
)

// Error codes of the ErrorResponse body. They are not HTTP statuses: the
// upstream answers 401 with errorCode 900.
const (
	CodeBadRequest   = 400
	CodeUnauthorized = 900
	CodeNotFound     = 404
	CodeInternal     = 500
)

// UnauthorizedMessage is the body message of a 401, as observed upstream.
const UnauthorizedMessage = "Access Denied - User not authenticated."

// ErrorResponse is the body of every error. IsLoggedOn is only emitted when the
// caller sets it: the upstream 401 carries it, the other errors do not.
type ErrorResponse struct {
	ErrorCode    int64  `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	IsLoggedOn   *bool  `json:"isLoggedOn,omitempty"`
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

// ModList is the body of every list endpoint. errorCode is -1 and errorMessage
// is empty on success: that is what every capture carries, including the ones
// that return data.
type ModList struct {
	SearchParams SearchParams `json:"searchParams"`
	ModList      []models.Mod `json:"modList"`
	TotalCount   *int64       `json:"totalCount"`
	IsLoggedOn   bool         `json:"isLoggedOn"`
	ErrorCode    int64        `json:"errorCode"`
	ErrorMessage string       `json:"errorMessage"`
}

// noErrorCode is the errorCode of a successful list response.
const noErrorCode = -1

// NewModList wraps a page of mods. Every list endpoint goes through it, so it
// is also the single place where the stored media names become absolute URLs.
// totalCount is null on an empty page: the captures answer null there, in
// every version, because the upstream COUNT(*) OVER () has no row to bind to.
func NewModList(r *http.Request, deps Deps, params SearchParams, page *models.ModPage, loggedOn bool) ModList {
	mods := page.Mods
	if mods == nil {
		mods = []models.Mod{}
	}
	AbsolutizeMods(r, deps, mods)
	var total *int64
	if len(mods) > 0 {
		value := page.TotalCount
		total = &value
	}
	return ModList{
		SearchParams: params,
		ModList:      mods,
		TotalCount:   total,
		IsLoggedOn:   loggedOn,
		ErrorCode:    noErrorCode,
	}
}

// LegacyModList is the envelope of the legacy list endpoints: the same shape
// as ModList but with the legacy row render.
type LegacyModList struct {
	SearchParams SearchParams       `json:"searchParams"`
	ModList      []models.LegacyMod `json:"modList"`
	TotalCount   *int64             `json:"totalCount"`
	IsLoggedOn   bool               `json:"isLoggedOn"`
	ErrorCode    int64              `json:"errorCode"`
	ErrorMessage string             `json:"errorMessage"`
}

// NewLegacyModList wraps a page of mods in the legacy row render. withTotal is
// false for My, which carries totalCount null in every capture; the other
// lists answer null on an empty page, where the upstream COUNT(*) OVER () has
// no row to bind to.
func NewLegacyModList(r *http.Request, deps Deps, params SearchParams, page *models.ModPage, loggedOn bool, withTotal bool) LegacyModList {
	mods := page.Mods
	if mods == nil {
		mods = []models.Mod{}
	}
	AbsolutizeMods(r, deps, mods)
	legacy := make([]models.LegacyMod, 0, len(mods))
	for _, mod := range mods {
		legacy = append(legacy, models.LegacyModOf(mod))
	}
	var total *int64
	if withTotal && len(mods) > 0 {
		value := page.TotalCount
		total = &value
	}
	return LegacyModList{
		SearchParams: params,
		ModList:      legacy,
		TotalCount:   total,
		IsLoggedOn:   loggedOn,
		ErrorCode:    noErrorCode,
	}
}

// WriteJSON writes a JSON body with the given status. Content-Type matches the
// official captures (space after the semicolon); i.JSON adds no extra bytes.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	i.JSON(w, body)
}

// ProblemDetails is the RFC 9110 error body the v4 API answers with when the
// resource does not exist: the capture of Detail for a deleted mod carries
// exactly type, title, status and traceId. traceId is a W3C trace id pair of
// 16 and 8 random bytes between the version and flag bytes.
type ProblemDetails struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	TraceID string `json:"traceId"`
}

// problemDocument returns the RFC 9110 fragment of a status and its title. The
// detail body carries the fragment of the section the status is defined in.
func problemDocument(status int) (title string, document string) {
	switch status {
	case http.StatusBadRequest:
		return "Bad Request", "https://tools.ietf.org/html/rfc9110#section-15.5.1"
	case http.StatusUnauthorized:
		return "Unauthorized", "https://tools.ietf.org/html/rfc9110#section-15.5.2"
	case http.StatusForbidden:
		return "Forbidden", "https://tools.ietf.org/html/rfc9110#section-15.5.4"
	case http.StatusNotFound:
		return "Not Found", "https://tools.ietf.org/html/rfc9110#section-15.5.5"
	case http.StatusMethodNotAllowed:
		return "Method Not Allowed", "https://tools.ietf.org/html/rfc9110#section-15.5.6"
	case http.StatusInternalServerError:
		return "Internal Server Error", "https://tools.ietf.org/html/rfc9110#section-15.6.1"
	default:
		return http.StatusText(status), "https://tools.ietf.org/html/rfc9110"
	}
}

// WriteProblemDetails writes the RFC 9110 error body of a status.
func WriteProblemDetails(w http.ResponseWriter, status int) {
	random := make([]byte, 24)
	if _, err := crand.Read(random); err != nil {
		random = make([]byte, 24)
	}
	trace := fmt.Sprintf("00-%x-%x-00", random[:16], random[16:])
	title, document := problemDocument(status)
	WriteJSON(w, status, ProblemDetails{
		Type:    document,
		Title:   title,
		Status:  status,
		TraceID: trace,
	})
}

// WriteError writes an ErrorResponse.
func WriteError(w http.ResponseWriter, status int, code int64, message string) {
	WriteJSON(w, status, ErrorResponse{ErrorCode: code, ErrorMessage: message})
}

// WriteUnauthorized writes the 401 body observed upstream, which carries
// isLoggedOn in addition to the error fields.
func WriteUnauthorized(w http.ResponseWriter) {
	loggedOn := false
	WriteJSON(w, http.StatusUnauthorized, ErrorResponse{
		ErrorCode:    CodeUnauthorized,
		ErrorMessage: UnauthorizedMessage,
		IsLoggedOn:   &loggedOn,
	})
}

// WriteStoreError maps a store error to the matching status.
func WriteStoreError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case isNotFound(err):
		WriteError(w, http.StatusNotFound, CodeNotFound, "not found")
	case isUnauthorized(err):
		WriteUnauthorized(w)
	case isValidation(err):
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error")
	}
}

func isNotFound(err error) bool     { return errors.Is(err, models.ErrNotFound) }
func isUnauthorized(err error) bool { return errors.Is(err, models.ErrUnauthorized) }
func isValidation(err error) bool   { return errors.Is(err, models.ErrValidation) }
