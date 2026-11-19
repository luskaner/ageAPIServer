// SPDX-License-Identifier: AGPL-3.0-only

package v1

import (
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// prefix is the canonical, lowercased prefix of the legacy API.
const prefix = "/api/v1/mods"

// Register wires every v1 endpoint. All of them require authentication: the
// spec declares no `security: []` for the legacy API.
func Register(group *router.Group, deps shared.Deps) {
	auth := &router.Authenticator{Users: deps.Stores.Users, CookieName: deps.CookieName}
	base := group.Subgroup(prefix)

	base.Handle("POST", "/findasuser", router.Chain(FindAsUser(deps), auth.Required()))
	base.Handle("POST", "/detail", router.Chain(Detail(deps), auth.Required()))
	base.Handle("POST", "/installed", router.Chain(Installed(deps), auth.Required()))
	base.Handle("POST", "/my", router.Chain(My(deps), auth.Required()))
	base.Handle("POST", "/flag", router.Chain(Flag(deps), auth.Required()))
	base.Handle("POST", "/install", router.Chain(Install(deps), auth.Required()))
	base.Handle("POST", "/uninstall", router.Chain(Uninstall(deps), auth.Required()))
	base.Handle("POST", "/create", router.Chain(Create(deps), auth.Required()))
	base.Handle("POST", "/delete", router.Chain(Delete(deps), auth.Required()))
}
