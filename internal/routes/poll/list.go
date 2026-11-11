package poll

import (
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// List serves GET /Poll/list.
func List(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		polls, err := deps.Stores.Polls.List(r.Context())
		if err != nil {
			shared.WriteStoreError(w, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, polls)
	}
}
