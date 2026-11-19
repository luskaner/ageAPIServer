// SPDX-License-Identifier: AGPL-3.0-only

// Package cmd is the process entry point: flags, configuration, logger,
// listener and shutdown. It is a consumer of the library API, not a second
// implementation of it.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/luskaner/ageAPIServer"
	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/cert"
	"github.com/luskaner/ageAPIServer/internal/logger"
)

// Version is set by main from the build-time value.
var Version = "development"

// defaultConfigPath is where the packaged configuration lives.
const defaultConfigPath = "resources/config/config.toml"

// Execute parses the flags and runs the server. It returns the error and the
// process exit code. args is os.Args[1:] in the binary; tests hand their own.
func Execute(args []string) (error, int) {
	fs := pflag.NewFlagSet("ageAPIServer", pflag.ContinueOnError)
	fs.SortFlags = false

	fs.Bool("version", false, "print the version and exit")

	fs.String("config", defaultConfigPath, "path to the TOML configuration file")
	fs.String("server.addr", "", "listen address (default :8080)")
	fs.String("server.publicbaseurl", "", "absolute base URL used to build downloadUrl")
	fs.String("server.tls.mode", "", "off, on or proxy (default off)")
	fs.String("server.tls.folder", "", "directory holding cert.pem and key.pem")
	fs.String("server.tls.cert", "", "certificate file (default <folder>/cert.pem)")
	fs.String("server.tls.key", "", "private key file (default <folder>/key.pem)")
	fs.String("db.path", "", "SQLite file (empty means in-memory)")
	fs.Bool("log.enabled", true, "write the log file")
	fs.String("log.level", "", "debug, info, warn or error")
	fs.String("log.file", "", "log file path")
	fs.Int64("log.maxsizebytes", 0, "rotate the log file at this size in bytes (0 disables)")
	fs.String("session.ttl", "", "web session lifetime")
	fs.String("session.cookieName", "", "session cookie name")
	fs.StringSlice("cors.allowedOrigins", nil, "allowed browser origins")
	fs.Bool("cors.allowCredentials", true, "send Access-Control-Allow-Credentials")
	fs.String("pagination.base", "", "page or offset")
	fs.Int("pagination.defaultCount", 0, "default page size")
	fs.Int("pagination.maxCount", 0, "maximum page size")
	fs.String("storage.uploaddir", "", "directory for published mod files")
	fs.Int64("storage.maxmodfilebytes", 0, "maximum size of an uploaded mod archive in bytes (0 disables)")
	fs.Bool("matchcontent.enabled", true, "serve the /api/MatchContent endpoints")
	fs.String("matchcontent.folder", "", "directory for match savegames and replays")
	fs.Int64("matchcontent.maxfilesize", 0, "maximum size of one match file in bytes")
	fs.Int64("matchcontent.maxtotalsize", 0, "maximum accumulated size of match files in bytes")
	fs.String("matchcontent.expiry", "", "lifetime of a match file")
	fs.Int("matchcontent.ratelimitperminute", 0, "maximum MatchContent requests per client IP per minute (0 disables)")
	fs.String("new-session", "", "print a session cookie value for a Steam token and exit")
	fs.Bool("gc-uploads", false, "report the files of Storage.UploadDir that no mod references, then exit")
	fs.Bool("gc-apply", false, "delete what --gc-uploads reported (ignored without it)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil, 0
		}
		return err, i.ErrConfig
	}
	// --version answers before the configuration is loaded: it must work
	// without a config file, a database or a log file.
	if version, err := fs.GetBool("version"); err == nil && version {
		fmt.Println("ageAPIServer " + Version)
		return nil, 0
	}
	return runRoot(fs)
}

func runRoot(fs *pflag.FlagSet) (error, int) {
	configPath, err := fs.GetString("config")
	if err != nil {
		return err, i.ErrConfig
	}
	cfg, usedFile, err := i.Load(configPath, fs)
	if err != nil {
		return err, i.ErrConfig
	}

	if err := logger.OpenMainFileLog(cfg.Log.File, cfg.Log.Enabled, cfg.Log.Level, cfg.Log.MaxSizeBytes); err != nil {
		return fmt.Errorf("open log file: %w", err), i.ErrCreateLogFile
	}
	defer logger.Close()

	logger.Banner(Version)
	logger.Section("Configuration")
	if usedFile != "" {
		logger.KV(0, "config file", usedFile)
	} else {
		logger.KV(0, "config file", "(defaults)")
	}
	logger.KV(0, "database", cfg.DB.Path)
	logger.KV(0, "listen", cfg.Server.Addr)
	logger.KV(0, "tls", tlsDescription(cfg))

	// The GC runs with the server stopped, before any listener exists: a
	// publish in flight could be writing one of the files it classifies.
	if gcUploads, err := fs.GetBool("gc-uploads"); err != nil {
		return err, i.ErrConfig
	} else if gcUploads {
		apply, err := fs.GetBool("gc-apply")
		if err != nil {
			return err, i.ErrConfig
		}
		return uploadGC(cfg, apply)
	}

	if token, err := fs.GetString("new-session"); err != nil {
		return err, i.ErrConfig
	} else if token != "" {
		return newSession(cfg, token)
	}

	pair, err := certificatePair(cfg)
	if err != nil {
		return fmt.Errorf("tls: %w", err), i.ErrCertificate
	}
	if cfg.TLSEnabled() {
		logger.KV(0, "certificate", pair.Cert)
		logger.KV(0, "private key", pair.Key)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return listenAndServe(ctx, cfg, pair)
}

// listenAndServe builds the API, binds the listener and serves until ctx is
// cancelled. It is the second half of runRoot, and this seam is what lets the
// tests exercise the construction and the graceful shutdown without a signal.
func listenAndServe(ctx context.Context, cfg i.Config, pair cert.Pair) (error, int) {
	server, err := ageAPIServer.New(ageAPIServer.Config{
		DBPath:     cfg.DB.Path,
		LogEnabled: cfg.Log.Enabled,
		SessionTTL: cfg.SessionTTLDuration(),
		CookieName: cfg.Session.CookieName,
		CORS: ageAPIServer.CORSConfig{
			AllowedOrigins:   cfg.CORS.AllowedOrigins,
			AllowCredentials: cfg.CORS.AllowCredentials,
			MaxAge:           cfg.CORSMaxAgeDuration(),
		},
		Pagination: ageAPIServer.PaginationConfig{
			Base:         cfg.Pagination.Base,
			DefaultCount: cfg.Pagination.DefaultCount,
			MaxCount:     cfg.Pagination.MaxCount,
		},
		PublicScheme:    cfg.PublicScheme(),
		UploadDir:       cfg.Storage.UploadDir,
		MaxModFileBytes: cfg.Storage.MaxModFileBytes,
		MatchContent: ageAPIServer.MatchContentConfig{
			Enabled:            cfg.MatchContent.Enabled,
			Folder:             cfg.MatchContent.Folder,
			MaxFileSize:        cfg.MatchContent.MaxFileSize,
			MaxTotalSize:       cfg.MatchContent.MaxTotalSize,
			Expiry:             cfg.MatchContentExpiryDuration(),
			RateLimitPerMinute: cfg.MatchContent.RateLimitPerMinute,
		},
	})
	if err != nil {
		return fmt.Errorf("build server: %w", err), i.ErrDatabase
	}
	defer func() { _ = server.Shutdown(context.Background()) }()

	listener, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Server.Addr, err), i.ErrStartServer
	}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeoutDuration(),
		IdleTimeout:       cfg.IdleTimeoutDuration(),
		ErrorLog:          logger.StdLogger(),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Ok("listening on %s (%d routes, %s)", listener.Addr().String(), len(server.Routes()), cfg.PublicScheme())
		if cfg.TLSEnabled() {
			serveErr <- httpServer.ServeTLS(listener, pair.Cert, pair.Key)
			return
		}
		serveErr <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err), i.ErrStartServer
		}
	case <-ctx.Done():
		logger.Section("Shutdown")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err), i.ErrStartServer
		}
		logger.Ok("stopped")
	}
	return nil, 0
}

// certificatePair resolves the pair the listener serves TLS with. It is only
// read when this process terminates TLS: in the other two modes no key is
// needed at all, so a deployment without one is complete rather than broken.
func certificatePair(cfg i.Config) (cert.Pair, error) {
	if !cfg.TLSEnabled() {
		return cert.Pair{}, nil
	}
	folder := cfg.Server.TLS.Folder
	if folder == "" {
		resolved := cert.Folder(os.Args[0])
		if resolved == "" {
			return cert.Pair{}, errors.New("failed to determine the certificate folder")
		}
		folder = resolved
	}
	return cert.Load(folder, cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
}

// tlsDescription is the one-line summary of the mode that the configuration
// section prints.
func tlsDescription(cfg i.Config) string {
	switch strings.ToLower(cfg.Server.TLS.Mode) {
	case "on":
		return "on (https terminated here)"
	case "proxy":
		return "proxy (external https terminator)"
	default:
		return "off (http)"
	}
}

// newSession prints a session cookie value for a Steam token. The spec has no
// login endpoint, so this is how an operator obtains a cookie for the website
// flow.
func newSession(cfg i.Config, steamToken string) (error, int) {
	stores, err := openStoresFn(cfg.DB.Path, cfg.DB.BusyTimeoutMs, cfg.DB.MaxOpenConns)
	if err != nil {
		return fmt.Errorf("open database: %w", err), i.ErrDatabase
	}
	defer func() { _ = closeStoresFn(stores) }()

	ctx := context.Background()
	user, err := stores.Users.UserBySteamToken(ctx, steamToken)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err), i.ErrInvalidAuthentication
	}
	token, expires, err := stores.Users.CreateSession(ctx, user.UserID, cfg.SessionTTLDuration())
	if err != nil {
		return fmt.Errorf("create session: %w", err), i.ErrDatabase
	}
	fmt.Printf("%s=%s\n", cfg.Session.CookieName, token)
	fmt.Printf("expires: %s\n", expires.Format(time.RFC3339))
	return nil, 0
}
