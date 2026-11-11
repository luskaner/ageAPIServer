// Package textmoderation answers the moderation probe of the game clients. The
// package path mirrors the endpoint: /textmoderation.
package textmoderation

import (
	"net/http"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Response is the TextModerationResponse schema.
type Response struct {
	FilterResult         string `json:"filterResult"`
	FamilyFriendlyResult string `json:"familyFriendlyResult"`
	MediumResult         string `json:"mediumResult"`
	MatureResult         string `json:"matureResult"`
	MaturePlusResult     string `json:"maturePlusResult"`
	TranslationAvailable bool   `json:"translationAvailable"`
	ModeratedText        string `json:"moderatedText"`
	Language             string `json:"language"`
}

// TextModeration serves POST /textmoderation. The upstream answers Allow for
// every input; the same is done here, and the text is echoed back as the
// moderated text so the client can keep its flow.
func TextModeration(_ shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request shared.TextModerationRequest
		if err := i.Bind(r, &request); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "invalid body")
			return
		}
		if err := request.Validate(); err != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, err.Error())
			return
		}
		shared.WriteJSON(w, http.StatusOK, Response{
			FilterResult:         "Allow",
			FamilyFriendlyResult: "Allow",
			MediumResult:         "Allow",
			MatureResult:         "Allow",
			MaturePlusResult:     "Allow",
			TranslationAvailable: false,
			ModeratedText:        request.TextContent,
			Language:             request.Language,
		})
	}
}
