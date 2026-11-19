package cmd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/cert"
	"github.com/luskaner/ageAPIServer/internal/logger"
)

func TestMain(m *testing.M) {
	// The command's banner and configuration section would otherwise reach the
	// real stdout of the test run.
	logger.Initialize(io.Discard)
	os.Exit(m.Run())
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original }()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(data)
}

func TestExecuteVersionPrintsTheVersionAndExits(t *testing.T) {
	previous := Version
	Version = "v9.9.9-test"
	t.Cleanup(func() { Version = previous })

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "should-not-exist.log")

	var err error
	var code int
	out := captureStdout(t, func() {
		err, code = Execute([]string{"--version", "--log.file", logPath})
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, "ageAPIServer "+Version) {
		t.Fatalf("stdout = %q", out)
	}
	// --version answers before the configuration is loaded, so no log file is
	// created.
	entries, readErr := os.ReadDir(logDir)
	if readErr != nil {
		t.Fatalf("read log dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("--version created %d files", len(entries))
	}
}

func TestExecuteHelpReturnsCleanly(t *testing.T) {
	err, code := Execute([]string{"--help"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestExecuteReportsABadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("[Server\nAddr = "), 0o644); err != nil {
		t.Fatal(err)
	}
	err, code := Execute([]string{"--config", path})
	if err == nil {
		t.Fatal("a malformed config file was accepted")
	}
	if code != i.ErrConfig {
		t.Fatalf("code = %d, want %d", code, i.ErrConfig)
	}
}

func TestExecuteRejectsAnInvalidFlag(t *testing.T) {
	err, code := Execute([]string{"--no-such-flag"})
	if err == nil {
		t.Fatal("invalid flag accepted")
	}
	if code != i.ErrConfig {
		t.Fatalf("code = %d, want %d", code, i.ErrConfig)
	}
}

// TestNewSessionPrintsTheCookie runs the --new-session flow against a real
// database file and a user seeded through a second handle.
func TestNewSessionPrintsTheCookie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedStores, err := openStoresFn(path, 5000, 1)
	if err != nil {
		t.Fatalf("seed stores: %v", err)
	}
	if _, err := seedStores.Users.UserBySteamToken(context.Background(), "session-token"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := closeStoresFn(seedStores); err != nil {
		t.Fatalf("close seed stores: %v", err)
	}

	cfg := i.DefaultConfig()
	cfg.DB.Path = path
	cfg.Session.CookieName = ".AgeAPIServerTest"

	var sessionErr error
	var code int
	out := captureStdout(t, func() {
		sessionErr, code = newSession(cfg, "session-token")
	})
	if sessionErr != nil {
		t.Fatalf("new session: %v", sessionErr)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, cfg.Session.CookieName+"=") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(out, "expires: ") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestTLSDescription(t *testing.T) {
	cases := map[string]string{
		"on":    "on (https terminated here)",
		"proxy": "proxy (external https terminator)",
		"off":   "off (http)",
	}
	for mode, want := range cases {
		cfg := i.DefaultConfig()
		cfg.Server.TLS.Mode = mode
		if got := tlsDescription(cfg); got != want {
			t.Fatalf("%s: got %q, want %q", mode, got, want)
		}
	}
}

// TestNewSessionRejectsAnEmptyToken covers the lookup failure the operator sees
// for a token that resolves no user.
func TestNewSessionRejectsAnEmptyToken(t *testing.T) {
	cfg := i.DefaultConfig()
	cfg.DB.Path = filepath.Join(t.TempDir(), "session.db")
	err, code := newSession(cfg, "")
	if err == nil {
		t.Fatal("an empty token was accepted")
	}
	if code != i.ErrInvalidAuthentication {
		t.Fatalf("code = %d, want %d", code, i.ErrInvalidAuthentication)
	}
}

// TestCertificatePairUsesTheDefaultFolder covers the branch that resolves
// resources/certificates next to the executable when only the file names are
// configured.
func TestCertificatePairUsesTheDefaultFolder(t *testing.T) {
	dir := t.TempDir()
	writeSelfSignedPair(t, dir)
	cfg := i.DefaultConfig()
	cfg.Server.TLS.Mode = "on"
	cfg.Server.TLS.Cert = filepath.Join(dir, cert.CertName)
	cfg.Server.TLS.Key = filepath.Join(dir, cert.KeyName)

	pair, err := certificatePair(cfg)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if pair.Cert != cfg.Server.TLS.Cert || pair.Key != cfg.Server.TLS.Key {
		t.Fatalf("pair = %+v", pair)
	}
}

// TestListenAndServeReportsABrokenTLSPair covers the ServeTLS error path: the
// listener never accepts, the process would start with an unusable pair.
func TestListenAndServeReportsABrokenTLSPair(t *testing.T) {
	cfg := i.DefaultConfig()
	cfg.DB.Path = filepath.Join(t.TempDir(), "tls.db")
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.TLS.Mode = "on"
	cfg.Log.Enabled = false
	missing := filepath.Join(t.TempDir(), "missing.pem")

	err, code := listenAndServe(context.Background(), cfg, cert.Pair{Cert: missing, Key: missing})
	if err == nil {
		t.Fatal("a missing PEM pair was served")
	}
	if code != i.ErrStartServer {
		t.Fatalf("code = %d, want %d", code, i.ErrStartServer)
	}
}

func TestCertificatePairIsSkippedWithoutTLS(t *testing.T) {
	cfg := i.DefaultConfig()
	pair, err := certificatePair(cfg)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if pair.Cert != "" || pair.Key != "" {
		t.Fatalf("pair = %+v, want empty", pair)
	}
}

// writeSelfSignedPair generates the PEM pair in a temporary folder. The project
// never generates certificates; this is a test fixture only.
func writeSelfSignedPair(t *testing.T, dir string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ageAPIServer test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(dir, cert.CertName), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cert.KeyName), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCertificatePairReadsThePEMPair(t *testing.T) {
	dir := t.TempDir()
	writeSelfSignedPair(t, dir)

	cfg := i.DefaultConfig()
	cfg.Server.TLS.Mode = "on"
	cfg.Server.TLS.Folder = dir
	pair, err := certificatePair(cfg)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if pair.Cert != filepath.Join(dir, cert.CertName) || pair.Key != filepath.Join(dir, cert.KeyName) {
		t.Fatalf("pair = %+v", pair)
	}
}

func TestExecuteReportsAnInvalidCertificate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := "[Server]\nAddr = \"127.0.0.1:0\"\n\n[Server.TLS]\nMode = \"on\"\nFolder = \"" +
		filepath.ToSlash(t.TempDir()) + "\"\n\n[Log]\nEnabled = false\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	err, code := Execute([]string{"--config", configPath})
	if err == nil {
		t.Fatal("a missing certificate pair was accepted")
	}
	if code != i.ErrCertificate {
		t.Fatalf("code = %d, want %d", code, i.ErrCertificate)
	}
}

// TestListenAndServeShutsDownOnACancelledContext drives the construction and
// the graceful shutdown without a signal or a fixed port.
func TestListenAndServeShutsDownOnACancelledContext(t *testing.T) {
	cfg := i.DefaultConfig()
	cfg.DB.Path = filepath.Join(t.TempDir(), "serve.db")
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Log.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err, code := listenAndServe(ctx, cfg, cert.Pair{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestListenAndServeReportsAFailedBind(t *testing.T) {
	cfg := i.DefaultConfig()
	cfg.DB.Path = filepath.Join(t.TempDir(), "serve.db")
	// A port outside the TCP range fails the bind on every platform, unlike a
	// privileged port that Windows lets a normal user bind.
	cfg.Server.Addr = "127.0.0.1:70000"
	cfg.Log.Enabled = false

	err, code := listenAndServe(context.Background(), cfg, cert.Pair{})
	if err == nil {
		t.Fatal("a reserved port was bound")
	}
	if code != i.ErrStartServer {
		t.Fatalf("code = %d, want %d", code, i.ErrStartServer)
	}
}

func TestListenAndServeReportsAnUnusableDatabase(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := i.DefaultConfig()
	cfg.DB.Path = blocker
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Log.Enabled = false

	err, code := listenAndServe(context.Background(), cfg, cert.Pair{})
	if err == nil {
		t.Fatal("a database path that is a file was accepted")
	}
	if code != i.ErrDatabase {
		t.Fatalf("code = %d, want %d", code, i.ErrDatabase)
	}
}

func TestExecuteReportsAnUnwritableLogFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := "[Server]\nAddr = \"127.0.0.1:0\"\n\n[Log]\nFile = \"" +
		filepath.ToSlash(filepath.Join(t.TempDir(), "missing", "app.log")) + "\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	err, code := Execute([]string{"--config", configPath})
	if err == nil {
		t.Fatal("an unwritable log file was accepted")
	}
	if code != i.ErrCreateLogFile {
		t.Fatalf("code = %d, want %d", code, i.ErrCreateLogFile)
	}
}
