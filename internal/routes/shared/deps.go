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
}
