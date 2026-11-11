package router

import (
	"net/http"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// Normalize rewrites the request path so the mux sees one canonical form. The
// spec requires case-insensitive matching and an optional trailing slash, and
// the clients exercise both: AOM sends /api/v4/mods/Detail/583271 and
// /api/v4/Mods/Detail/457105 in the same session, and the website sends
// /api/v4/mods/UnSubscribe/890.
func Normalize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawPath != "" {
			// An encoded slash would create a path segment the mux never sees;
			// refuse it instead of guessing.
			if strings.Contains(strings.ToLower(r.URL.RawPath), "%2f") {
				shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "encoded slash in path")
				return
			}
			r.URL.RawPath = ""
		}

		cleaned, ok := cleanPath(r.URL.Path)
		if !ok {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "path escapes the root")
			return
		}
		r.URL.Path = cleaned
		next.ServeHTTP(w, r)
	})
}

// cleanPath collapses repeated slashes, resolves . and .., trims the trailing
// slash and lowercases the result. It reports false when the path escapes the
// root.
func cleanPath(path string) (string, bool) {
	if path == "" {
		return "/", true
	}
	segments := make([]string, 0, 8)
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			if len(segments) == 0 {
				return "", false
			}
			segments = segments[:len(segments)-1]
		default:
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return "/", true
	}
	return strings.ToLower("/" + strings.Join(segments, "/")), true
}
