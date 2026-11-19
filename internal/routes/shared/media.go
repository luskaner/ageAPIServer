// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// MediaPath is the route that serves the images stored by Publish.
const MediaPath = "/api/v4/mods/image/"

// MediaURL renders an absolute URL for a stored image name. A value that is
// already absolute is returned untouched: it may point at a CDN. The upstream
// answer carries URLs here, not bare names, so a client that does not resolve
// them against the API host would render nothing.
func MediaURL(r *http.Request, deps Deps, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || models.IsAbsoluteURL(name) {
		return name
	}
	return BaseURL(r, deps.PublicBaseURL, deps.PublicScheme) + MediaPath + url.PathEscape(name)
}

// AbsolutizeMods rewrites the media names of one page in place.
func AbsolutizeMods(r *http.Request, deps Deps, mods []models.Mod) {
	for i := range mods {
		mods[i].Thumbnail = MediaURL(r, deps, mods[i].Thumbnail)
		for j := range mods[i].ImageURLs {
			mods[i].ImageURLs[j].ImageName = MediaURL(r, deps, mods[i].ImageURLs[j].ImageName)
			mods[i].ImageURLs[j].ImageThumbnail = MediaURL(r, deps, mods[i].ImageURLs[j].ImageThumbnail)
		}
	}
}

// AbsolutizeDetail rewrites the media names of one mod in place. The captures
// render both the image name and its thumbnail as absolute URLs.
func AbsolutizeDetail(r *http.Request, deps Deps, detail *models.Detail) {
	if detail == nil {
		return
	}
	if detail.Thumbnail != nil {
		if thumbnail := MediaURL(r, deps, *detail.Thumbnail); thumbnail == "" {
			detail.Thumbnail = nil
		} else {
			detail.Thumbnail = &thumbnail
		}
	}
	if detail.FileURL != nil && *detail.FileURL != "" {
		detail.FileURL = new(DownloadURL(r, deps, detail.ModID, *detail.FileURL))
	}
	for i := range detail.ImageURLs {
		detail.ImageURLs[i].ImageName = MediaURL(r, deps, detail.ImageURLs[i].ImageName)
		detail.ImageURLs[i].ImageThumbnail = MediaURL(r, deps, detail.ImageURLs[i].ImageThumbnail)
	}
}
