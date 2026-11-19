package shared

import (
	"net/http"
	"time"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// Deps is what a route package needs to serve a request. It carries no
// listener settings: those belong to the command, not to the handlers.
type Deps struct {
	Stores        *models.Stores
	Pagination    Pagination
	PublicBaseURL string
	SessionTTL    time.Duration
	CookieName    string
	SameSite      http.SameSite
	// RequireAuthForReadOnly turns the endpoints the spec declares anonymous
	// into authenticated ones.
	RequireAuthForReadOnly bool
	// UploadDir is where Publish stores the mod file and its images. Empty
	// rejects any request that carries a file.
	UploadDir string
	// MatchContent is the store behind the /api/MatchContent endpoints. Nil or
	// disabled answers 404.
	MatchContent *models.CustomFilesStore
}
