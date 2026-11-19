// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// Healthz answers the operations probe. It is outside the spec on purpose: the
// game clients never call it, a deployment needs it.
func Healthz(system *models.SystemStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := system.Ping(); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
