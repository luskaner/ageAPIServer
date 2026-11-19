package cert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFolderCreatesTheConventionalPath(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ageAPIServer.exe")
	folder := Folder(executable)

	want := filepath.Join(filepath.Dir(executable), ResourcesDir, FolderName)
	if folder != want {
		t.Fatalf("folder = %q, want %q", folder, want)
	}
	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("stat folder: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", folder)
	}
}

func TestFolderWithoutAnExecutablePath(t *testing.T) {
	if folder := Folder(""); folder != "" {
		t.Fatalf("folder = %q, want empty", folder)
	}
}

func TestLoadFallsBackToTheConventionalNames(t *testing.T) {
	folder := t.TempDir()
	write(t, filepath.Join(folder, CertName))
	write(t, filepath.Join(folder, KeyName))

	pair, err := Load(folder, "", "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pair.Cert != filepath.Join(folder, CertName) || pair.Key != filepath.Join(folder, KeyName) {
		t.Fatalf("pair = %+v", pair)
	}
}

func TestLoadPrefersExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	certPath := write(t, filepath.Join(dir, "issued.crt"))
	keyPath := write(t, filepath.Join(dir, "issued.key"))

	pair, err := Load(filepath.Join(dir, "unused"), certPath, keyPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pair.Cert != certPath || pair.Key != keyPath {
		t.Fatalf("pair = %+v", pair)
	}
}

func TestLoadReportsTheMissingFile(t *testing.T) {
	folder := t.TempDir()
	write(t, filepath.Join(folder, CertName))

	_, err := Load(folder, "", "")
	if err == nil {
		t.Fatal("missing key accepted")
	}
	if !strings.Contains(err.Error(), "private key") || !strings.Contains(err.Error(), KeyName) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadNeedsAFolderWithoutExplicitPaths(t *testing.T) {
	if _, err := Load("", "", ""); err == nil {
		t.Fatal("empty load accepted")
	}
}
