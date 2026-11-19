// Package cert resolves the certificate pair of the HTTPS listener. It keeps
// the layout of the sibling ageLANServer — cert.pem and key.pem inside
// resources/certificates next to the executable — and nothing else: this
// project does not generate certificates, the operator brings the pair (one
// issued by a CA, or any self-signed pair made elsewhere).
package cert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ResourcesDir is the packaged resources directory next to the executable. The
// configuration file lives in it too.
const ResourcesDir = "resources"

// FolderName is where the pair lives inside ResourcesDir.
const FolderName = "certificates"

// CertName and KeyName are the conventional names of the pair.
const (
	CertName = "cert.pem"
	KeyName  = "key.pem"
)

// Pair is the certificate and private key the listener serves TLS with.
type Pair struct {
	Cert string
	Key  string
}

// Folder resolves <executable dir>/resources/certificates and creates it, so an
// operator can drop the pair in without hunting for the path. It returns "" when
// the path cannot be resolved or created.
func Folder(executablePath string) string {
	if executablePath == "" {
		return ""
	}
	parent := filepath.Dir(executablePath)
	if parent == "" {
		return ""
	}
	folder := filepath.Join(parent, ResourcesDir, FolderName)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return ""
	}
	return folder
}

// Load validates the pair. Explicit paths win; an empty one falls back to the
// conventional name inside folder. The error names the offending file, which is
// all an operator needs to fix the startup.
func Load(folder, certPath, keyPath string) (Pair, error) {
	if folder == "" && (certPath == "" || keyPath == "") {
		return Pair{}, errors.New("no certificate folder: set the folder or both file paths")
	}
	pair := Pair{Cert: certPath, Key: keyPath}
	if pair.Cert == "" {
		pair.Cert = filepath.Join(folder, CertName)
	}
	if pair.Key == "" {
		pair.Key = filepath.Join(folder, KeyName)
	}
	if err := readable(pair.Cert); err != nil {
		return Pair{}, fmt.Errorf("certificate: %w", err)
	}
	if err := readable(pair.Key); err != nil {
		return Pair{}, fmt.Errorf("private key: %w", err)
	}
	return pair, nil
}

func readable(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}
