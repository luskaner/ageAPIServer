// SPDX-License-Identifier: AGPL-3.0-only

// Package textmoderation answers the moderation probe of the game clients. The
// package path mirrors the endpoint: /textmoderation.
package textmoderation

import (
	"net/http"
	"strings"

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

// baseLanguage drops the region of an input language tag: the upstream answer
// carries the detected language ("en" for "Test Mod" even with "es-ES" as the
// input) and the base tag is the closest deterministic value.
func baseLanguage(tag string) string {
	if base, _, found := strings.Cut(tag, "-"); found {
		return base
	}
	return tag
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
			// The answer carries the detected language, not the echo of the
			// request: the captures answer "en" for "Test Mod" whatever the
			// input language was. The base subtag is the deterministic stand-in.
			Language: baseLanguage(request.Language),
		})
	}
}
