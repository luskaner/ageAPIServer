// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"time"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// Deps is what a route package needs to serve a request. It carries no
// listener settings: those belong to the command, not to the handlers.
type Deps struct {
	Stores        *models.Stores
	Pagination    Pagination
	PublicBaseURL string
	// PublicScheme is the scheme absolute URLs fall back to when the request
	// does not name one. Empty means "http".
	PublicScheme string
	SessionTTL   time.Duration
	CookieName   string
	// UploadDir is where Publish stores the mod file and its images. Empty
	// rejects any request that carries a file.
	UploadDir string
	// MaxModFileBytes caps an uploaded archive or screenshot as it is streamed
	// to disk. Zero means no limit.
	MaxModFileBytes int64
	// MatchContent is the store behind the /api/MatchContent endpoints. Nil or
	// disabled answers 404.
	MatchContent *models.CustomFilesStore
	// MatchContentRateLimitPerMinute caps the requests one client IP may make
	// to the MatchContent endpoints per minute. Zero disables the limit.
	MatchContentRateLimitPerMinute int
}
