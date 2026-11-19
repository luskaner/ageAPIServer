// SPDX-License-Identifier: AGPL-3.0-only

package poll

import (
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Register wires the poll endpoints. The spec declares no `security: []` for
// them, so they inherit the global requirement and need authentication.
func Register(group *router.Group, deps shared.Deps) {
	auth := &router.Authenticator{Users: deps.Stores.Users, CookieName: deps.CookieName}

	group.Handle("GET", "/poll/list", router.Chain(List(deps), auth.Required()))
	group.Handle("GET", "/poll/{pollid}", router.Chain(Get(deps), auth.Required()))
}
