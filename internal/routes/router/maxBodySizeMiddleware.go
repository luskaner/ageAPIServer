package router

import (
	"net/http"
	"path"
	"strings"
)

// exemptBodySizePrefixes are the routes whose handler enforces its own limit.
// A multiplayer savegame is measured in megabytes, so the global cap would
// truncate it before the handler could accept it: MatchContent streams the body
// to disk and applies its own [MatchContent] MaxFileSize. The CheckAndPublishFile
// blocks carry a whole mod archive the same way (the capture block is 1.5 MB)
// and stream to disk under the handler's own block limit.
var exemptBodySizePrefixes = []string{"/api/matchcontent", "/api/v4/mods/checkandpublishfile"}

// exemptBodySizePath matches against the cleaned path. The chain runs after
// Normalize, but a direct use of the middleware may see the raw form, and a
// traversal such as "/api/matchcontent/../x" must keep the global cap because
// it lands on a handler that does not apply its own.
func exemptBodySizePath(urlPath string) bool {
	cleaned := strings.ToLower(path.Clean(urlPath))
	for _, prefix := range exemptBodySizePrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return true
		}
	}
	return false
}

// MaxBodySize bounds the request body so a client cannot force an unbounded
// allocation. The routes in exemptBodySizePrefixes are left to the handler,
// which knows its own, larger limit.
func MaxBodySize(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit > 0 && r.Body != nil && !exemptBodySizePath(r.URL.Path) {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
