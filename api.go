// SPDX-License-Identifier: AGPL-3.0-only

// Package ageAPIServer implements the Age of Empires Mods API (api_spec.yml)
// on top of SQLite. It is usable as a standalone binary and as a mountable
// http.Handler inside any existing net/http application.
package ageAPIServer

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/luskaner/ageAPIServer/internal/logger"
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/models/initializer"
	"github.com/luskaner/ageAPIServer/internal/routes/api/mods/v1"
	"github.com/luskaner/ageAPIServer/internal/routes/api/mods/v4"
	"github.com/luskaner/ageAPIServer/internal/routes/matchcontent"
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
	// UploadDir is where Publish stores the mod file and its images. Empty
	// rejects any publish that carries a file.
	UploadDir string
	// Console receives the per-request log line. Nil keeps the current sink
	// (stdout by default); io.Discard keeps the output of the host application
	// clean.
	Console io.Writer
	// PublicBaseURL is the absolute base of the download and media URLs. Empty
	// derives it from each request.
	PublicBaseURL string
	// PublicScheme is the scheme derived URLs fall back to when neither the
	// request nor a proxy header names one. Empty means "http"; "https" is for
	// a deployment behind an external TLS terminator that forwards only the
	// host.
	PublicScheme string
	// MaxBodySize caps a request body. Zero means 1 MiB. Publish and the legacy
	// Create are exempt: their archive is streamed to disk under
	// MaxModFileBytes, and /api/MatchContent/ applies MatchContent.MaxFileSize.
	MaxBodySize int64
	// MaxModFileBytes caps each archive or screenshot Publish streams to
	// UploadDir. Zero means no limit.
	MaxModFileBytes int64
	// MatchContent is the disk store behind the /api/MatchContent endpoints.
	// The zero value leaves them answering 404.
	MatchContent MatchContentConfig
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

// MatchContentConfig is the disk store behind /api/MatchContent, where the
// game clients exchange multiplayer savegames and replays.
type MatchContentConfig struct {
	// Enabled turns the endpoints on. When false they answer 404.
	Enabled bool
	// Folder is where the files are kept. Empty or "auto" means
	// <os.TempDir()>/ageAPIServer/custom-files.
	Folder string
	// MaxFileSize is the per-file limit in bytes. Zero means no per-file limit.
	MaxFileSize int64
	// MaxTotalSize evicts the oldest files when exceeded. Zero means no
	// accumulated limit.
	MaxTotalSize int64
	// Expiry deletes a file after this long. Zero keeps it until it is evicted.
	Expiry time.Duration
	// RateLimitPerMinute caps the requests one client IP may make to the
	// MatchContent endpoints per minute. Zero disables the limit.
	RateLimitPerMinute int
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
	stores       *models.Stores
	matchContent *models.CustomFilesStore
	handler      http.Handler
	routes       []Route
}

// DefaultCookieName is the session cookie of the website.
const DefaultCookieName = ".AgeOfEmpiresServices"

// New builds the stores and the handler.
func New(cfg Config) (*Server, error) {
	stores, err := initializer.Initialize(cfg.DBPath, 5000, 1)
	if err != nil {
		return nil, err
	}
	// The console is process-wide, so it is set once here and not per request.
	// Nil leaves whatever the process (or the binary) configured.
	if cfg.Console != nil {
		logger.Initialize(cfg.Console)
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

	maxBodySize := cfg.MaxBodySize
	if maxBodySize <= 0 {
		maxBodySize = 1 << 20
	}

	matchContent := &models.CustomFilesStore{}
	matchContent.Initialize(&models.CustomFilesOpts{
		Enabled:      cfg.MatchContent.Enabled,
		Folder:       cfg.MatchContent.Folder,
		MaxFileSize:  cfg.MatchContent.MaxFileSize,
		MaxTotalSize: cfg.MatchContent.MaxTotalSize,
		Expiry:       cfg.MatchContent.Expiry,
	})

	deps := shared.Deps{
		Stores:          stores,
		Pagination:      pagination,
		PublicBaseURL:   cfg.PublicBaseURL,
		PublicScheme:    cfg.PublicScheme,
		SessionTTL:      sessionTTL,
		CookieName:      cookieName,
		UploadDir:       cfg.UploadDir,
		MaxModFileBytes: cfg.MaxModFileBytes,
		MatchContent:    matchContent,
		// The rate limit is a route concern: the store only owns the files.
		MatchContentRateLimitPerMinute: cfg.MatchContent.RateLimitPerMinute,
	}

	group := router.New()
	v4.Register(group, deps)
	v1.Register(group, deps)
	poll.Register(group, deps)
	textmoderation.Register(group, deps)
	matchcontent.Register(group, deps)
	group.HandleFunc("GET", "/openapi.yml", openAPISpec)
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
			AllowedMethods:   inventoryMethods(routeInventory),
		}),
		router.MaxBodySize(maxBodySize),
	)

	if cfg.Logger != nil {
		cfg.Logger.Debug("ageAPIServer ready", "db", cfg.DBPath, "routes", len(routeInventory))
	}

	return &Server{stores: stores, matchContent: matchContent, handler: handler, routes: routeInventory}, nil
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

// Shutdown releases the database and the match file store.
func (s *Server) Shutdown(_ context.Context) error {
	s.matchContent.Close()
	return s.stores.Close()
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	shared.WriteError(w, http.StatusNotFound, shared.CodeNotFound, "not found")
}

// inventoryMethods renders the methods the inventory actually serves for the
// Access-Control-Allow-Methods header. The spec serves a PUT
// (CheckAndPublishFile), which a hardcoded GET/POST/OPTIONS list would hide
// from the browser preflight.
func inventoryMethods(routes []Route) []string {
	served := make(map[string]bool, len(routes))
	for _, route := range routes {
		served[strings.ToLower(route.Method)] = true
	}
	methods := make([]string, 0, len(served)+1)
	for _, method := range []string{"get", "post", "put", "delete"} {
		if served[method] {
			methods = append(methods, strings.ToUpper(method))
		}
	}
	return append(methods, http.MethodOptions)
}

// routeInventory is the documented list of endpoints. It is the single source
// of truth for Routes() and for the all-routes test.
var routeInventory = []Route{
	{Method: "GET", Path: "/api/v4/mods/games", SpecPath: "/api/v4/mods/Games", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/tags", SpecPath: "/api/v4/mods/Tags", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/featured", SpecPath: "/api/v4/mods/Featured", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/find", SpecPath: "/api/v4/Mods/Find/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/detail/{modid}", SpecPath: "/api/v4/Mods/Detail/{modId}", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/related/{modid}", SpecPath: "/api/v4/mods/Related/{modId}", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/reviews", SpecPath: "/api/v4/mods/Reviews", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/installed", SpecPath: "/api/v4/Mods/Installed/", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/my", SpecPath: "/api/v4/Mods/My/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/subscribe/{modid}", SpecPath: "/api/v4/Mods/Subscribe/{modId}", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/unsubscribe/{modid}", SpecPath: "/api/v4/Mods/Unsubscribe/{modId}", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/rate", SpecPath: "/api/v4/Mods/Rate/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/download/{modid}", SpecPath: "/api/v4/Mods/Download/{modId}/", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/download/{modid}/file", SpecPath: "/api/v4/mods/Download/{modId}/file", AuthRequired: true},
	{Method: "GET", Path: "/api/v4/mods/image/{name}", SpecPath: "/api/v4/mods/Image/{name}"},
	{Method: "POST", Path: "/api/v4/mods/publish", SpecPath: "/api/v4/mods/Publish", AuthRequired: true},
	{Method: "PUT", Path: "/api/v4/mods/checkandpublishfile", SpecPath: "/api/v4/mods/CheckAndPublishFile", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/delete", SpecPath: "/api/v4/mods/Delete", AuthRequired: true},
	{Method: "POST", Path: "/api/v4/mods/report", SpecPath: "/api/v4/mods/Report", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/detail", SpecPath: "/api/v1/Mods/Detail/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/findasuser", SpecPath: "/api/v1/Mods/FindAsUser/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/installed", SpecPath: "/api/v1/Mods/Installed/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/my", SpecPath: "/api/v1/Mods/My/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/create", SpecPath: "/api/v1/Mods/Create/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/delete", SpecPath: "/api/v1/Mods/Delete/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/flag", SpecPath: "/api/v1/Mods/Flag/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/install", SpecPath: "/api/v1/Mods/Install/", AuthRequired: true},
	{Method: "POST", Path: "/api/v1/mods/uninstall", SpecPath: "/api/v1/Mods/Uninstall/", AuthRequired: true},
	{Method: "GET", Path: "/poll/list", SpecPath: "/Poll/list", AuthRequired: true},
	{Method: "GET", Path: "/poll/{pollid}", SpecPath: "/Poll/{pollId}", AuthRequired: true},
	{Method: "POST", Path: "/textmoderation", SpecPath: "/textmoderation", AuthRequired: true},
	{Method: "POST", Path: "/api/matchcontent", SpecPath: "/api/MatchContent/"},
	{Method: "GET", Path: "/api/matchcontent/{matchid}", SpecPath: "/api/MatchContent/{matchId}"},
	{Method: "GET", Path: "/api/matchcontent/{matchid}/file/{hash}", SpecPath: "/api/MatchContent/{matchId}/file/{hash}"},
	{Method: "GET", Path: "/healthz", SpecPath: "/healthz"},
	// Local Ops addition: the embedded api_spec.yml.
	{Method: "GET", Path: "/openapi.yml", SpecPath: "/openapi.yml"},
}
