// SPDX-License-Identifier: AGPL-3.0-only

package textmoderation

import (
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Register wires POST /textmoderation. The spec declares no `security: []`, so
// it inherits the global requirement.
func Register(group *router.Group, deps shared.Deps) {
	auth := &router.Authenticator{Users: deps.Stores.Users, CookieName: deps.CookieName}
	group.Handle("POST", "/textmoderation", router.Chain(TextModeration(deps), auth.Required()))
}
