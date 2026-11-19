// SPDX-License-Identifier: AGPL-3.0-only

package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoadDefaults(t *testing.T) {
	cfg, usedFile, err := Load("", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if usedFile != "" {
		t.Fatalf("used file %q", usedFile)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Pagination.Base != "page" || cfg.Pagination.MaxCount != 20000 {
		t.Fatalf("pagination = %+v", cfg.Pagination)
	}
	if cfg.Session.CookieName != ".AgeOfEmpiresServices" {
		t.Fatalf("cookie = %q", cfg.Session.CookieName)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("AGEAPISERVER_SERVER_ADDR", ":9999")
	t.Setenv("AGEAPISERVER_DB_PATH", "custom.db")
	t.Setenv("AGEAPISERVER_PAGINATION_MAXCOUNT", "500")

	cfg, _, err := Load("", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Addr != ":9999" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.DB.Path != "custom.db" {
		t.Fatalf("db path = %q", cfg.DB.Path)
	}
	if cfg.Pagination.MaxCount != 500 {
		t.Fatalf("max count = %d", cfg.Pagination.MaxCount)
	}
}

func TestLoadFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[Server]\nAddr = \":1234\"\n\n[Log]\nLevel = \"debug\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, usedFile, err := Load(path, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if usedFile != path {
		t.Fatalf("used file = %q", usedFile)
	}
	if cfg.Server.Addr != ":1234" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("level = %q", cfg.Log.Level)
	}
	// Keys the file does not set keep their default.
	if cfg.DB.Path != "ageAPIServer.db" {
		t.Fatalf("db path = %q", cfg.DB.Path)
	}
}

func TestLoadTLSDefaultsToPlainHTTP(t *testing.T) {
	cfg, _, err := Load("", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.TLS.Mode != "off" {
		t.Fatalf("mode = %q", cfg.Server.TLS.Mode)
	}
	if cfg.TLSEnabled() {
		t.Fatal("TLS reported as terminated here")
	}
	if cfg.PublicScheme() != "http" {
		t.Fatalf("scheme = %q", cfg.PublicScheme())
	}
}

func TestLoadTLSModes(t *testing.T) {
	cases := map[string]struct {
		tlsEnabled bool
		scheme     string
	}{
		"on":    {tlsEnabled: true, scheme: "https"},
		"proxy": {tlsEnabled: false, scheme: "https"},
		"off":   {tlsEnabled: false, scheme: "http"},
	}
	for mode, want := range cases {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGEAPISERVER_SERVER_TLS_MODE", mode)
			cfg, _, err := Load("", nil)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if cfg.Server.TLS.Mode != mode {
				t.Fatalf("mode = %q", cfg.Server.TLS.Mode)
			}
			if cfg.TLSEnabled() != want.tlsEnabled {
				t.Fatalf("tls enabled = %v, want %v", cfg.TLSEnabled(), want.tlsEnabled)
			}
			if cfg.PublicScheme() != want.scheme {
				t.Fatalf("scheme = %q, want %q", cfg.PublicScheme(), want.scheme)
			}
		})
	}
}

func TestLoadRejectsUnknownTLSMode(t *testing.T) {
	t.Setenv("AGEAPISERVER_SERVER_TLS_MODE", "maybe")
	if _, _, err := Load("", nil); err == nil {
		t.Fatal("unknown TLS mode accepted")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("AGEAPISERVER_LOG_LEVEL", "loud")
	if _, _, err := Load("", nil); err == nil {
		t.Fatal("invalid log level accepted")
	}
}

// TestValidateRejectsEveryBrokenField is the table of the validation gates: one
// case per rejected key, plus the valid default.
func TestValidateRejectsEveryBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"addr", func(c *Config) { c.Server.Addr = "" }},
		{"max body", func(c *Config) { c.Server.MaxBodyBytes = 0 }},
		{"read header timeout", func(c *Config) { c.Server.ReadHeaderTimeout = "nope" }},
		{"idle timeout", func(c *Config) { c.Server.IdleTimeout = "nope" }},
		{"tls mode", func(c *Config) { c.Server.TLS.Mode = "maybe" }},
		{"db max open conns", func(c *Config) { c.DB.MaxOpenConns = 0 }},
		{"db busy timeout", func(c *Config) { c.DB.BusyTimeoutMs = -1 }},
		{"log level", func(c *Config) { c.Log.Level = "loud" }},
		{"log max size", func(c *Config) { c.Log.MaxSizeBytes = -1 }},
		{"session ttl", func(c *Config) { c.Session.TTL = "nope" }},
		{"cookie name", func(c *Config) { c.Session.CookieName = "" }},
		{"cors max age", func(c *Config) { c.CORS.MaxAge = "nope" }},
		{"pagination base", func(c *Config) { c.Pagination.Base = "rows" }},
		{"pagination default count", func(c *Config) { c.Pagination.DefaultCount = -1 }},
		{"pagination max count", func(c *Config) { c.Pagination.MaxCount = 0 }},
		{"max mod file bytes", func(c *Config) { c.Storage.MaxModFileBytes = -1 }},
		{"match max file size", func(c *Config) { c.MatchContent.MaxFileSize = -1 }},
		{"match max total size", func(c *Config) { c.MatchContent.MaxTotalSize = -1 }},
		{"match expiry", func(c *Config) { c.MatchContent.Expiry = "nope" }},
		{"match rate limit", func(c *Config) { c.MatchContent.RateLimitPerMinute = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid value accepted")
			}
		})
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
}

func TestLoadStorageDefaults(t *testing.T) {
	cfg, _, err := Load("", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.MaxModFileBytes != 1<<30 {
		t.Fatalf("max mod file bytes = %d", cfg.Storage.MaxModFileBytes)
	}
	if cfg.Log.MaxSizeBytes != 0 {
		t.Fatalf("log max size = %d", cfg.Log.MaxSizeBytes)
	}
	if cfg.MatchContent.RateLimitPerMinute != 0 {
		t.Fatalf("rate limit = %d", cfg.MatchContent.RateLimitPerMinute)
	}
}

func TestLoadRejectsAConfigPathThatIsADirectory(t *testing.T) {
	if _, _, err := Load(t.TempDir(), nil); err == nil {
		t.Fatal("a directory was accepted as a config file")
	}
}

func TestLoadIgnoresMissingFile(t *testing.T) {
	cfg, usedFile, err := Load(filepath.Join(t.TempDir(), "absent.toml"), nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if usedFile != "" {
		t.Fatalf("used file = %q", usedFile)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
}

// TestEnvironmentOverridesFile is the regression of the case-sensitive koanf
// key bug: the file writes "Server.Addr" and the environment "server.addr", and
// the environment must win regardless of the merge order.
func TestEnvironmentOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[Server]\nAddr = \":1234\"\n\n[DB]\nPath = \"from-file.db\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGEAPISERVER_SERVER_ADDR", ":4321")

	for attempt := range 20 {
		cfg, _, err := Load(path, nil)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.Server.Addr != ":4321" {
			t.Fatalf("attempt %d: addr = %q, want :4321", attempt, cfg.Server.Addr)
		}
		if cfg.DB.Path != "from-file.db" {
			t.Fatalf("attempt %d: db path = %q", attempt, cfg.DB.Path)
		}
	}
}

// TestLoadMatchContentConfig pins the defaults of the /api/MatchContent store
// and that the TOML section reaches the struct.
func TestLoadMatchContentConfig(t *testing.T) {
	cfg, _, err := Load("", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.MatchContent.Enabled || cfg.MatchContent.Folder != "auto" {
		t.Fatalf("match content = %+v", cfg.MatchContent)
	}
	if cfg.MatchContent.MaxFileSize != 32<<20 || cfg.MatchContent.MaxTotalSize != 256<<20 {
		t.Fatalf("match content sizes = %+v", cfg.MatchContent)
	}
	if cfg.MatchContentExpiryDuration() != 12*time.Hour {
		t.Fatalf("expiry = %s", cfg.MatchContentExpiryDuration())
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[MatchContent]\nEnabled = false\nFolder = \"match-store\"\nMaxFileSize = 1024\nMaxTotalSize = 4096\nExpiry = \"30m\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(path, nil)
	if err != nil {
		t.Fatalf("load file: %v", err)
	}
	if cfg.MatchContent.Enabled {
		t.Fatal("MatchContent.Enabled did not come from the file")
	}
	if cfg.MatchContent.Folder != "match-store" || cfg.MatchContent.MaxFileSize != 1024 ||
		cfg.MatchContent.MaxTotalSize != 4096 || cfg.MatchContentExpiryDuration() != 30*time.Minute {
		t.Fatalf("match content from file = %+v", cfg.MatchContent)
	}
}

// TestUnsetFlagsKeepTheirDefaults is the regression for the posflag merge: an
// unset flag must not inject its empty default over DefaultConfig.
func TestUnsetFlagsKeepTheirDefaults(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("server.addr", "", "")
	fs.String("log.level", "", "")
	fs.String("session.ttl", "", "")
	fs.Int("pagination.maxcount", 0, "")
	if err := fs.Parse([]string{"--server.addr", ":1234"}); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load("", fs)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Addr != ":1234" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("log level = %q, want the default info", cfg.Log.Level)
	}
	if cfg.Session.TTL != "720h" {
		t.Fatalf("session ttl = %q, want the default 720h", cfg.Session.TTL)
	}
	if cfg.Pagination.MaxCount != 20000 {
		t.Fatalf("max count = %d, want the default 20000", cfg.Pagination.MaxCount)
	}
}
