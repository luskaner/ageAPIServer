package v4

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// prefix is the canonical, lowercased prefix. Normalize has already lowercased
// the request path, so the mux only ever sees this form.
const prefix = "/api/v4/mods"

// Register wires every v4 endpoint. The read-only ones are anonymous but still
// resolve the identity when credentials are present, so isLoggedOn and the
// user* fields are filled in.
func Register(group *router.Group, deps shared.Deps) {
	auth := &router.Authenticator{Users: deps.Stores.Users, CookieName: deps.CookieName}
	base := group.Subgroup(prefix)

	base.Handle("GET", "/games", router.Chain(Games(deps), auth.Optional()))
	base.Handle("GET", "/tags", router.Chain(Tags(deps), auth.Optional()))
	base.Handle("POST", "/find", router.Chain(Find(deps), auth.Optional()))
	base.Handle("GET", "/detail/{modid}", router.Chain(Detail(deps), auth.Optional()))
	base.Handle("GET", "/related/{modid}", router.Chain(Related(deps), auth.Optional()))
	base.Handle("POST", "/featured", router.Chain(Featured(deps), auth.Optional()))
	base.Handle("POST", "/reviews", router.Chain(Reviews(deps), auth.Optional()))

	base.Handle("POST", "/installed", router.Chain(Installed(deps), auth.Required()))
	base.Handle("POST", "/my", router.Chain(My(deps), auth.Required()))
	base.Handle("GET", "/subscribe/{modid}", router.Chain(Subscribe(deps), auth.Required()))
	base.Handle("GET", "/unsubscribe/{modid}", router.Chain(Unsubscribe(deps), auth.Required()))
	base.Handle("POST", "/rate", router.Chain(Rate(deps), auth.Required()))
	base.Handle("GET", "/download/{modid}", router.Chain(Download(deps), auth.Required()))
}

// modIDFromPath parses the {modid} path value.
func modIDFromPath(r *http.Request) (int64, error) {
	return models.ParseModID(r.PathValue("modid"))
}
