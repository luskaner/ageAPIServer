package internal

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// EnvPrefix is the prefix of every environment variable that overrides the
// configuration file.
const EnvPrefix = "AGEAPISERVER_"

// Server holds the process-level HTTP settings. They are not part of the
// library Config: a host application owns its own listener.
type Server struct {
	Addr              string
	ReadHeaderTimeout string
	IdleTimeout       string
	MaxBodyBytes      int64
	// PublicBaseURL is the absolute base used to build downloadUrl. Empty means
	// the URL is rebuilt from the request (X-Forwarded-* aware).
	PublicBaseURL string
}

// DB holds the SQLite settings.
type DB struct {
	Path          string
	BusyTimeoutMs int
	MaxOpenConns  int
}

// Log holds the file logger settings.
type Log struct {
	Enabled bool
	Level   string
	File    string
}

// Session holds the web session cookie settings.
type Session struct {
	TTL        string
	CookieName string
	SameSite   string
}

// CORS holds the browser access settings. The website sends a preflight before
// every POST, so this is required for it to work at all.
type CORS struct {
	AllowedOrigins   []string
	AllowCredentials bool
	MaxAge           string
}

// Pagination holds the interpretation of start/count and the page size bounds.
type Pagination struct {
	// Base is "page" (start is a 1-based page number) or "offset" (start is a
	// row offset). The captures support "page"; "offset" is the escape hatch.
	Base         string
	DefaultCount int
	MaxCount     int
}

// Auth holds the authentication policy.
type Auth struct {
	// RequireForReadOnly makes the endpoints the spec declares anonymous
	// (Games, Tags, Featured, Find, Detail, Related, Reviews) reject requests
	// without credentials. The spec says they are anonymous and the default
	// keeps that, but a live probe of api.ageofempires.com answered 401 on
	// every one of them except Games and Tags. Set this to match that
	// behaviour once it is confirmed.
	RequireForReadOnly bool
}

// Storage holds where Publish writes the uploaded files.
type Storage struct {
	// UploadDir is the directory for mod archives and screenshots. Empty
	// rejects any publish that carries a file.
	UploadDir string
}

// Config is the process configuration, loaded from TOML, environment and flags.
type Config struct {
	Server     Server
	DB         DB
	Log        Log
	Session    Session
	CORS       CORS
	Pagination Pagination
	Auth       Auth
	Storage    Storage
}

// DefaultConfig returns the values used when neither the file nor the
// environment nor the flags set a key.
func DefaultConfig() Config {
	return Config{
		Server: Server{
			Addr:              ":8080",
			ReadHeaderTimeout: "5s",
			IdleTimeout:       "60s",
			MaxBodyBytes:      1 << 20,
			PublicBaseURL:     "",
		},
		DB: DB{
			Path:          "ageAPIServer.db",
			BusyTimeoutMs: 5000,
			MaxOpenConns:  1,
		},
		Log: Log{
			Enabled: true,
			Level:   "info",
			File:    "ageAPIServer.log",
		},
		Session: Session{
			TTL:        "720h",
			CookieName: ".AgeOfEmpiresServices",
			SameSite:   "Lax",
		},
		CORS: CORS{
			AllowedOrigins:   []string{"https://www.ageofempires.com"},
			AllowCredentials: true,
			MaxAge:           "600s",
		},
		Pagination: Pagination{
			Base:         "page",
			DefaultCount: 30,
			MaxCount:     20000,
		},
		Auth: Auth{
			RequireForReadOnly: false,
		},
		Storage: Storage{
			UploadDir: "uploads",
		},
	}
}

// Load reads the configuration from the TOML file (when it exists), then the
// environment, then the flags, and validates the result. It returns the path of
// the file that was actually used, empty when none existed.
func Load(configPath string, fs *pflag.FlagSet) (Config, string, error) {
	cfg := DefaultConfig()
	k := koanf.New(".")

	usedFile := ""
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			// The file is read into its own koanf so its keys can be lowercased
			// before they are merged: koanf keys are case-sensitive, so
			// "Server.Addr" from the file and "server.addr" from the
			// environment would otherwise be two different keys and the merge
			// order would decide the winner.
			fileKoanf := koanf.New(".")
			if err := fileKoanf.Load(file.Provider(configPath), toml.Parser()); err != nil {
				return cfg, "", fmt.Errorf("read config file %s: %w", configPath, err)
			}
			if err := k.Load(confmap.Provider(lowerKeys(fileKoanf.Raw()), "."), nil); err != nil {
				return cfg, "", fmt.Errorf("merge config file %s: %w", configPath, err)
			}
			usedFile = configPath
		} else if !errors.Is(err, os.ErrNotExist) {
			return cfg, "", fmt.Errorf("stat config file %s: %w", configPath, err)
		}
	}

	if err := k.Load(env.Provider(EnvPrefix, ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, EnvPrefix)), "_", ".")
	}), nil); err != nil {
		return cfg, usedFile, fmt.Errorf("read environment: %w", err)
	}

	if fs != nil {
		// Only the flags the user actually set are merged. posflag also merges
		// the default value of every unset flag whose key is not already in the
		// map, and the defaults declared in cmd are empty strings, so they would
		// wipe the values of DefaultConfig whenever the config file is missing
		// or partial.
		if err := k.Load(posflag.ProviderWithFlag(fs, ".", k, func(f *pflag.Flag) (string, any) {
			if !f.Changed {
				return "", nil
			}
			return strings.ToLower(f.Name), posflag.FlagVal(fs, f)
		}), nil); err != nil {
			return cfg, usedFile, fmt.Errorf("read flags: %w", err)
		}
	}

	if err := k.Unmarshal("", &cfg); err != nil {
		return cfg, usedFile, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, usedFile, err
	}
	return cfg, usedFile, nil
}

// Validate rejects values that would only fail later, at the first request.
func (c Config) Validate() error {
	if c.Server.Addr == "" {
		return errors.New("config: Server.Addr is required")
	}
	if c.Server.MaxBodyBytes <= 0 {
		return errors.New("config: Server.MaxBodyBytes must be positive")
	}
	if _, err := time.ParseDuration(c.Server.ReadHeaderTimeout); err != nil {
		return fmt.Errorf("config: Server.ReadHeaderTimeout: %w", err)
	}
	if _, err := time.ParseDuration(c.Server.IdleTimeout); err != nil {
		return fmt.Errorf("config: Server.IdleTimeout: %w", err)
	}
	if c.DB.MaxOpenConns < 1 {
		return errors.New("config: DB.MaxOpenConns must be at least 1")
	}
	if c.DB.BusyTimeoutMs < 0 {
		return errors.New("config: DB.BusyTimeoutMs must not be negative")
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: Log.Level %q is not one of debug, info, warn, error", c.Log.Level)
	}
	if _, err := time.ParseDuration(c.Session.TTL); err != nil {
		return fmt.Errorf("config: Session.TTL: %w", err)
	}
	if c.Session.CookieName == "" {
		return errors.New("config: Session.CookieName is required")
	}
	switch strings.ToLower(c.Session.SameSite) {
	case "lax", "strict", "none":
	default:
		return fmt.Errorf("config: Session.SameSite %q is not one of Lax, Strict, None", c.Session.SameSite)
	}
	if _, err := time.ParseDuration(c.CORS.MaxAge); err != nil {
		return fmt.Errorf("config: CORS.MaxAge: %w", err)
	}
	switch strings.ToLower(c.Pagination.Base) {
	case "page", "offset":
	default:
		return fmt.Errorf("config: Pagination.Base %q is not one of page, offset", c.Pagination.Base)
	}
	if c.Pagination.DefaultCount < 0 {
		return errors.New("config: Pagination.DefaultCount must not be negative")
	}
	if c.Pagination.MaxCount < 1 {
		return errors.New("config: Pagination.MaxCount must be at least 1")
	}
	return nil
}

// ReadHeaderTimeoutDuration is the parsed Server.ReadHeaderTimeout.
func (c Config) ReadHeaderTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.Server.ReadHeaderTimeout)
	return d
}

// IdleTimeoutDuration is the parsed Server.IdleTimeout.
func (c Config) IdleTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.Server.IdleTimeout)
	return d
}

// SessionTTLDuration is the parsed Session.TTL.
func (c Config) SessionTTLDuration() time.Duration {
	d, _ := time.ParseDuration(c.Session.TTL)
	return d
}

// CORSMaxAgeDuration is the parsed CORS.MaxAge.
func (c Config) CORSMaxAgeDuration() time.Duration {
	d, _ := time.ParseDuration(c.CORS.MaxAge)
	return d
}

// SameSiteMode maps Session.SameSite to the net/http constant.
func (c Config) SameSiteMode() http.SameSite {
	switch strings.ToLower(c.Session.SameSite) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// lowerKeys lowercases every key of a nested map, so the three configuration
// sources (file, environment, flags) always address the same koanf keys.
func lowerKeys(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		if nested, ok := value.(map[string]any); ok {
			out[strings.ToLower(key)] = lowerKeys(nested)
			continue
		}
		out[strings.ToLower(key)] = value
	}
	return out
}
