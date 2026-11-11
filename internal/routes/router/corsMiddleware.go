package router

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CORSConfig is the browser access policy. The website sends a preflight before
// every POST, so without this middleware it cannot call the API at all.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// CORS answers the preflight before the mux and adds the headers to the real
// response. It never answers with a wildcard origin while credentials are
// allowed: the browser rejects that combination.
func CORS(cfg CORSConfig) Middleware {
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[strings.ToLower(strings.TrimSpace(origin))] = true
	}
	maxAge := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin != "" && allowed[strings.ToLower(origin)] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "content-type, authorization")
				w.Header().Set("Access-Control-Max-Age", maxAge)
			}

			// The preflight is answered here: a POST-only pattern would give
			// 405 from the mux, and the browser would block the real request.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
