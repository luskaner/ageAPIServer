// SPDX-License-Identifier: AGPL-3.0-only

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

// Register wires every v4 endpoint. The read endpoints require credentials too:
// a live probe of api.ageofempires.com answered 401 on all of them, so the
// identity is always available for isLoggedOn and the user* fields.
func Register(group *router.Group, deps shared.Deps) {
	auth := &router.Authenticator{Users: deps.Stores.Users, CookieName: deps.CookieName}
	base := group.Subgroup(prefix)

	base.Handle("GET", "/games", router.Chain(Games(deps), auth.Required()))
	base.Handle("GET", "/tags", router.Chain(Tags(deps), auth.Required()))
	base.Handle("POST", "/find", router.Chain(Find(deps), auth.Required()))
	base.Handle("GET", "/detail/{modid}", router.Chain(Detail(deps), auth.Required()))
	base.Handle("GET", "/related/{modid}", router.Chain(Related(deps), auth.Required()))
	base.Handle("POST", "/featured", router.Chain(Featured(deps), auth.Required()))
	base.Handle("POST", "/reviews", router.Chain(Reviews(deps), auth.Required()))

	base.Handle("POST", "/installed", router.Chain(Installed(deps), auth.Required()))
	base.Handle("POST", "/my", router.Chain(My(deps), auth.Required()))
	base.Handle("GET", "/subscribe/{modid}", router.Chain(Subscribe(deps), auth.Required()))
	base.Handle("GET", "/unsubscribe/{modid}", router.Chain(Unsubscribe(deps), auth.Required()))
	base.Handle("POST", "/rate", router.Chain(Rate(deps), auth.Required()))
	base.Handle("GET", "/download/{modid}", router.Chain(Download(deps), auth.Required()))
	base.Handle("GET", "/download/{modid}/file", router.Chain(DownloadFile(deps), auth.Required()))
	base.Handle("GET", "/image/{name}", router.Chain(Image(deps), auth.Optional()))
	base.Handle("POST", "/publish", router.Chain(Publish(deps), auth.Required()))
	base.Handle("PUT", "/checkandpublishfile", router.Chain(CheckAndPublishFile(deps), auth.Required()))
	base.Handle("POST", "/delete", router.Chain(Delete(deps), auth.Required()))
	base.Handle("POST", "/report", router.Chain(Report(deps), auth.Required()))
}

// modIDFromPath parses the {modid} path value.
func modIDFromPath(r *http.Request) (int64, error) {
	return models.ParseModID(r.PathValue("modid"))
}
