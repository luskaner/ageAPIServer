// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// BaseURL resolves the absolute base of the deployment. A configured value
// wins; otherwise the request is used, honouring the X-Forwarded-* headers a
// reverse proxy sets. Without this, a deployment behind a proxy hands the
// client an internal host it cannot reach.
//
// scheme is what to answer when neither the connection nor a proxy header names
// one: empty means "http", and the deployment behind an external HTTPS
// terminator passes "https" so a proxy that forwards only the host still gets
// https links.
func BaseURL(r *http.Request, configured, scheme string) string {
	if configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	if scheme == "" {
		scheme = "http"
	}
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return scheme + "://" + host
}

// DownloadURL builds the absolute download URL of a mod. A stored absolute
// fileUrl wins: it may point at a CDN.
func DownloadURL(r *http.Request, deps Deps, modID int64, storedFileURL string) string {
	if models.IsAbsoluteURL(storedFileURL) {
		return storedFileURL
	}
	return BaseURL(r, deps.PublicBaseURL, deps.PublicScheme) + "/api/v4/Mods/Download/" + strconv.FormatInt(modID, 10) + "/file"
}

// DownloadInfo renders the SubscribeResponse/DownloadResponse body of a mod.
func DownloadInfo(r *http.Request, deps Deps, target *models.DownloadTarget) models.DownloadInfo {
	installCount := target.InstallCount
	return models.DownloadInfo{
		DownloadURL:   DownloadURL(r, deps, target.ModID, target.FileURL),
		DownloadCount: target.DownloadCount,
		InstallCount:  &installCount,
		GameTitleID:   target.GameTitleID,
		FileCRC:       target.FileCRC,
	}
}
