package internal

import (
	"os"
	"path/filepath"
	"testing"
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

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("AGEAPISERVER_LOG_LEVEL", "loud")
	if _, _, err := Load("", nil); err == nil {
		t.Fatal("invalid log level accepted")
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

func TestSameSiteMode(t *testing.T) {
	cases := map[string]int{
		"Lax":    2,
		"Strict": 3,
		"None":   4,
	}
	for value, want := range cases {
		cfg := DefaultConfig()
		cfg.Session.SameSite = value
		if got := int(cfg.SameSiteMode()); got != want {
			t.Fatalf("%s: got %d, want %d", value, got, want)
		}
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

	for attempt := 0; attempt < 20; attempt++ {
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
