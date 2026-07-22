// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/apikey.go.
package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// APIKeyHeader is the HTTP header callers must set when API key
// authentication is enabled via APIKeyAuth.
const APIKeyHeader = "X-API-Key" //nolint:gosec // G101 false positive: this is a header name, not a credential value

// noAuthPaths lists exact request paths that stay reachable without an API
// key even when authentication is enabled, so container/orchestrator health
// probes don't need a credential.
var noAuthPaths = map[string]bool{
	"/api/health": true,
}

// APIKeyAuth returns middleware that requires a valid API key — supplied via
// the X-API-Key header — on every request except health checks and the
// built-in docs. Keys are compared with a constant-time comparison so a
// partial match can't be timed byte-by-byte.
//
// If keys is empty the returned middleware is a no-op: API key
// authentication is opt-in. The gateway is designed to run behind a reverse
// proxy that handles authentication and TLS termination; when deployed
// without one — for example a bare `docker run` publishing the port directly
// to a network without another access control layer in front of it — set
// API_KEYS so the gateway enforces its own check instead of relying on an
// assumption about the deployment topology.
func APIKeyAuth(keys []string, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(keys) == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if noAuthPaths[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/docs") {
				next.ServeHTTP(w, r)
				return
			}

			supplied := r.Header.Get(APIKeyHeader)
			if supplied == "" || !matchesAnyKey(supplied, keys) {
				log.Warn("rejected request with missing or invalid API key",
					zap.String("path", r.URL.Path),
					zap.String("requestID", FromContext(r.Context())),
				)
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"details": "a valid " + APIKeyHeader + " header is required",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// matchesAnyKey reports whether supplied matches any of keys. Each candidate
// is checked with subtle.ConstantTimeCompare so comparison time does not
// reveal how many leading bytes of a guess were correct.
func matchesAnyKey(supplied string, keys []string) bool {
	for _, k := range keys {
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(k)) == 1 {
			return true
		}
	}
	return false
}
