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
func BaseURL(r *http.Request, configured string) string {
	if configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	scheme := "http"
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
func DownloadURL(r *http.Request, configured string, modID int64, storedFileURL string) string {
	if strings.HasPrefix(storedFileURL, "http://") || strings.HasPrefix(storedFileURL, "https://") {
		return storedFileURL
	}
	return BaseURL(r, configured) + "/api/v4/Mods/Download/" + strconv.FormatInt(modID, 10) + "/file"
}

// DownloadInfo renders the SubscribeResponse/DownloadResponse body of a mod.
func DownloadInfo(r *http.Request, deps Deps, target *models.DownloadTarget) models.DownloadInfo {
	installCount := target.InstallCount
	return models.DownloadInfo{
		DownloadURL:   DownloadURL(r, deps.PublicBaseURL, target.ModID, target.FileURL),
		DownloadCount: target.DownloadCount,
		InstallCount:  &installCount,
		GameTitleID:   target.GameTitleID,
		FileCRC:       target.FileCRC,
	}
}
