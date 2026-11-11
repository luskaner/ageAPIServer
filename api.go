// Package ageAPIServer implements the Age of Empires Mods API (api_spec.yml)
// on top of SQLite. It is usable as a standalone binary and as a mountable
// http.Handler inside any existing net/http application.
package ageAPIServer

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/models/initializer"
	"github.com/luskaner/ageAPIServer/internal/routes/api/mods/v1"
	"github.com/luskaner/ageAPIServer/internal/routes/api/mods/v4"
	"github.com/luskaner/ageAPIServer/internal/routes/poll"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
	"github.com/luskaner/ageAPIServer/internal/routes/textmoderation"
)

// DefaultSessionTTL is the lifetime of a web session cookie: 30 days, like the
// real one.
const DefaultSessionTTL = 720 * time.Hour

// Config is the library configuration. It carries nothing about the process
// (listen address, signals, instance lock): that belongs to the command.
type Config struct {
	// DBPath is the SQLite file. Empty means an in-memory database.
	DBPath string
	// LogEnabled turns on the file logger of the internal logger package.
	LogEnabled bool
	// Logger receives one line per construction. Nil discards it.
	Logger *slog.Logger
	// SessionTTL is the lifetime of a web session. Zero means DefaultSessionTTL.
	SessionTTL time.Duration
	// CookieName is the session cookie name. Empty means the upstream name.
	CookieName string
	// CORS is the browser access policy.
	CORS CORSConfig
	// Pagination is the interpretation of start/count.
	Pagination PaginationConfig
}

// CORSConfig is the browser access policy.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// PaginationConfig is the interpretation of start/count and the page bounds.
type PaginationConfig struct {
	// Base is "page" (default) or "offset".
	Base         string
	DefaultCount int
	MaxCount     int
}

// Route is one entry of the inventory.
type Route struct {
	// Method is the HTTP method.
	Method string
	// Path is the internal, lowercased pattern registered in the mux.
	Path string
	// AuthRequired reports whether the endpoint rejects anonymous requests.
	AuthRequired bool
	// SpecPath is the path as declared in api_spec.yml, with its own casing.
	SpecPath string
}

// Server is a constructed API instance.
type Server struct {
	stores  *models.Stores
	handler http.Handler
	routes  []Route
}

// DefaultCookieName is the session cookie of the website.
const DefaultCookieName = ".AgeOfEmpiresServices"

// New builds the stores and the handler.
func New(cfg Config) (*Server, error) {
	stores, err := initializer.Initialize(cfg.DBPath, 5000, 1)
	if err != nil {
		return nil, err
	}

	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = DefaultSessionTTL
	}
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = DefaultCookieName
	}
	origins := cfg.CORS.AllowedOrigins
	if len(origins) == 0 {
		origins = []string{"https://www.ageofempires.com"}
	}
	maxAge := cfg.CORS.MaxAge
	if maxAge <= 0 {
		maxAge = 600 * time.Second
	}
	pagination := shared.Pagination{
		Base:         cfg.Pagination.Base,
		DefaultCount: cfg.Pagination.DefaultCount,
		MaxCount:     cfg.Pagination.MaxCount,
	}
	if pagination.Base == "" {
		pagination.Base = "page"
	}
	if pagination.DefaultCount <= 0 {
		pagination.DefaultCount = 30
	}
	if pagination.MaxCount <= 0 {
		pagination.MaxCount = 20000
	}

	deps := shared.Deps{
		Stores:     stores,
		Pagination: pagination,
		SessionTTL: sessionTTL,
		CookieName: cookieName,
	}

	group := router.New()
	v4.Register(group, deps)
	v1.Register(group, deps)
	poll.Register(group, deps)
	textmoderation.Register(group, deps)
	group.Handle("GET", "/healthz", shared.Healthz(stores.System))
	group.HandlePath("/", http.HandlerFunc(notFound))

	handler := router.Chain(group.Mux(),
		router.Recover,
		router.Logger,
		router.Normalize,
		router.CORS(router.CORSConfig{
			AllowedOrigins:   origins,
			AllowCredentials: cfg.CORS.AllowCredentials,
			MaxAge:           maxAge,
		}),
		router.MaxBodySize(1<<20),
	)

	if cfg.Logger != nil {
		cfg.Logger.Debug("ageAPIServer ready", "db", cfg.DBPath, "routes", len(routeInventory))
	}

	return &Server{stores: stores, handler: handler, routes: routeInventory}, nil
}

// Handler returns the mountable handler. Mount it under a prefix with
// http.StripPrefix; the normalizer trims the trailing slash, so the handler
// must tolerate "/" (it answers 404 JSON, it does not panic).
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Routes returns the inventory of endpoints.
func (s *Server) Routes() []Route {
	out := make([]Route, len(s.routes))
	copy(out, s.routes)
	return out
}

// Shutdown releases the database.
func (s *Server) Shutdown(_ context.Context) error {
	return s.stores.Close()
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "not found")
}

// routeInventory is the documented list of endpoints. It is the single source
// of truth for Routes() and for the all-routes test.
var routeInventory = []Route{
	{Method: "GET", Path: "/api/v4/mods/games", SpecPath: "/api/v4/mods/Games"},
	{Method: "GET", Path: "/api/v4/mods/tags", SpecPath: "/api/v4/mods/Tags"},
	{Method: "POST", Path: "/api/v4/mods/featured", SpecPath: "/api/v4/mods/Featured"},
	{Method: "POST", Path: "/api/v4/mods/find", SpecPath: "/api/v4/Mods/Find/"},
	{Method: "GET", Path: "/api/v4/mods/detail/{modid}", SpecPath: "/api/v4/Mods/Detail/{modId}"},
	{Method: "GET", Path: "/api/v4/mods/related/{modid}", SpecPath: "/api/v4/mods/Related/{modId}"},
	{Method: "POST", Path: "/api/v4/mods/reviews", SpecPath: "/api/v4/mods/Reviews"},
	{Method: "POST", Path: "/api/v4/mods/installed", SpecPath: "/api/v4/Mods/Installed/", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/my", SpecPath: "/api/v4/Mods/My/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/subscribe/{modid}", SpecPath: "/api/v4/Mods/Subscribe/{modId}", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/unsubscribe/{modid}", SpecPath: "/api/v4/Mods/Unsubscribe/{modId}", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/rate", SpecPath: "/api/v4/Mods/Rate/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/download/{modid}", SpecPath: "/api/v4/Mods/Download/{modId}/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/detail", SpecPath: "/api/v1/Mods/Detail/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/findasuser", SpecPath: "/api/v1/Mods/FindAsUser/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/installed", SpecPath: "/api/v1/Mods/Installed/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/my", SpecPath: "/api/v1/Mods/My/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/flag", SpecPath: "/api/v1/Mods/Flag/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/install", SpecPath: "/api/v1/Mods/Install/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/uninstall", SpecPath: "/api/v1/Mods/Uninstall/", AuthRequired: true},
	{Method: "GET", Path: "/poll/list", SpecPath: "/Poll/list", AuthRequired: true},
	{Method: "GET", Path: "/poll/{pollid}", SpecPath: "/Poll/{pollId}", AuthRequired: true},
	{Method: "POST", Path: "/textmoderation", SpecPath: "/textmoderation", AuthRequired: true},
	{Method: "GET", Path: "/healthz", SpecPath: ""},
}
