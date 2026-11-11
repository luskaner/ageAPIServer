// Package initializer builds the stores from the process configuration. It is
// the single place that knows how a database path becomes a set of stores, so
// the command and the tests construct them the same way.
package initializer

import (
	"github.com/luskaner/ageAPIServer/internal/models"
)

// Initialize opens the database, applies the schema and seeds the catalogs.
func Initialize(path string, busyTimeoutMs int, maxOpenConns int) (*models.Stores, error) {
	return models.Open(models.Options{
		Path:          path,
		BusyTimeoutMs: busyTimeoutMs,
		MaxOpenConns:  maxOpenConns,
	})
}
