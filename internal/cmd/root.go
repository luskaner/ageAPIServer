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
	"syscall"
	"time"

	"github.com/spf13/pflag"

	ageAPIServer "github.com/luskaner/ageAPIServer"
	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/logger"
)

// Version is set by main from the build-time value.
var Version = "development"

// defaultConfigPath is where the packaged configuration lives.
const defaultConfigPath = "resources/config/config.toml"

// Execute parses the flags and runs the server. It returns the error and the
// process exit code.
func Execute() (error, int) {
	fs := pflag.NewFlagSet("ageAPIServer", pflag.ContinueOnError)
	fs.SortFlags = false

	fs.String("config", defaultConfigPath, "path to the TOML configuration file")
	fs.String("server.addr", "", "listen address (default :8080)")
	fs.String("server.publicbaseurl", "", "absolute base URL used to build downloadUrl")
	fs.String("db.path", "", "SQLite file (empty means in-memory)")
	fs.Bool("log.enabled", true, "write the log file")
	fs.String("log.level", "", "debug, info, warn or error")
	fs.String("log.file", "", "log file path")
	fs.String("session.ttl", "", "web session lifetime")
	fs.String("session.cookieName", "", "session cookie name")
	fs.String("session.sameSite", "", "Lax, Strict or None")
	fs.StringSlice("cors.allowedOrigins", nil, "allowed browser origins")
	fs.Bool("cors.allowCredentials", true, "send Access-Control-Allow-Credentials")
	fs.String("pagination.base", "", "page or offset")
	fs.Int("pagination.defaultCount", 0, "default page size")
	fs.Int("pagination.maxCount", 0, "maximum page size")
	fs.String("storage.uploaddir", "", "directory for published mod files")
	fs.String("new-session", "", "print a session cookie value for a Steam token and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil, 0
		}
		return err, i.ErrConfig
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

	if err := logger.OpenMainFileLog(cfg.Log.File, cfg.Log.Enabled, cfg.Log.Level); err != nil {
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

	if token, err := fs.GetString("new-session"); err != nil {
		return err, i.ErrConfig
	} else if token != "" {
		return newSession(cfg, token)
	}

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
		RequireAuthForReadOnly: cfg.Auth.RequireForReadOnly,
		UploadDir:              cfg.Storage.UploadDir,
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
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Ok("listening on %s (%d routes)", listener.Addr().String(), len(server.Routes()))
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
