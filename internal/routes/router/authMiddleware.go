package router

import (
	"net/http"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// steamScheme is the Authorization scheme of the game clients. It contains a
// dot, so r.BasicAuth() cannot parse it.
const steamScheme = "Steam1.0 "

// Authenticator resolves the identity of a request from the Steam token or the
// web session cookie.
type Authenticator struct {
	Users      *models.UserStore
	CookieName string
}

// Resolve returns the identity of the request. It never fails for a missing
// credential: that is an anonymous request.
func (a *Authenticator) Resolve(r *http.Request) (models.Viewer, error) {
	if header := r.Header.Get("Authorization"); header != "" {
		if token, found := strings.CutPrefix(header, steamScheme); found {
			user, err := a.Users.UserBySteamToken(r.Context(), token)
			if err != nil {
				return models.Viewer{}, err
			}
			return models.Viewer{UserID: user.UserID, SteamID: user.SteamID, LoggedOn: true}, nil
		}
	}
	if a.CookieName != "" {
		if cookie, err := r.Cookie(a.CookieName); err == nil && cookie.Value != "" {
			user, err := a.Users.UserBySessionToken(r.Context(), cookie.Value)
			if err != nil {
				return models.Viewer{}, err
			}
			return models.Viewer{UserID: user.UserID, SteamID: user.SteamID, LoggedOn: true}, nil
		}
	}
	return models.Viewer{}, nil
}

// Middleware attaches the identity to the context. When required is true, a
// request without a valid credential is answered with 401.
func (a *Authenticator) Middleware(required bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			viewer, err := a.Resolve(r)
			if err != nil && required {
				shared.WriteUnauthorized(w)
				return
			}
			if err != nil {
				// An anonymous route with a bad credential is still anonymous.
				viewer = models.Viewer{}
			}
			if required && !viewer.LoggedOn {
				shared.WriteUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithViewer(r.Context(), viewer)))
		})
	}
}

// Optional attaches the identity when present and never rejects.
func (a *Authenticator) Optional() Middleware {
	return a.Middleware(false)
}

// Required rejects anonymous requests.
func (a *Authenticator) Required() Middleware {
	return a.Middleware(true)
}
