// SPDX-License-Identifier: AGPL-3.0-only

// Package matchcontent implements the /api/MatchContent/ endpoints, where the
// game clients upload and download multiplayer savegames and replays.
//
// The payload is streamed to disk and never buffered in memory: the host
// uploads the file before it knows the match id, so the handler stages the
// bytes and renames them into place once the "matchId" part has been read.
package matchcontent

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// prefix is the lowercased, trailing-slash-free prefix. Normalize has already
// rewritten the request path by the time the mux sees it, so the trailing slash
// the game sends is gone and the upload route is the bare prefix.
const prefix = "/api/matchcontent"

// Register wires the three endpoints. They are anonymous: the game does not
// carry a session cookie when it uploads a savegame. When the operator sets a
// rate limit, the per-IP bucket wraps all three; the zero limit leaves the
// chain exactly as the captures saw it.
func Register(group *router.Group, deps shared.Deps) {
	limiter := newRateLimiter(deps.MatchContentRateLimitPerMinute)
	wrap := func(handler http.HandlerFunc) http.Handler {
		if limiter == nil {
			return handler
		}
		return router.Chain(handler, limiter.middleware())
	}
	group.Handle("POST", prefix, wrap(Upload(deps)))
	group.Handle("GET", prefix+"/{matchid}", wrap(Download(deps)))
	group.Handle("GET", prefix+"/{matchid}/file/{hash}", wrap(DownloadFile(deps)))
}
