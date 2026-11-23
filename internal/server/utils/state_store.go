package utils

import (
	"net/http"

	"GoTodo/internal/statestore"
)

// stateStore returns the server's state store (Redis or Postgres), or nil
// before startup has connected one, as in unit tests.
func stateStore() statestore.Store {
	return statestore.Current()
}

// StateStoreAvailable reports whether a state store is connected.
func StateStoreAvailable() bool {
	return stateStore() != nil
}

const stateStoreUnavailableMsg = "The REST API is unavailable: the server has no state store (Redis or Postgres) for authentication and rate limiting."

// RequireStateStore fails closed when no state store is connected.
func RequireStateStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !StateStoreAvailable() {
			APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable", stateStoreUnavailableMsg)
			return
		}
		next(w, r)
	}
}
