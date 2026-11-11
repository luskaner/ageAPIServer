package cmd

import (
	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/models/initializer"
)

// The seams exist so the command can be tested without a real listener or a
// real database file.
var (
	openStoresFn  = initializer.Initialize
	closeStoresFn = func(stores *models.Stores) error {
		return stores.Close()
	}
)
