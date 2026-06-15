// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/request_id.go.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/kristiannissen/carrier-gateway/internal/requestid"
)

// RequestIDHeader is the HTTP header used to propagate the correlation ID.
// Callers may send this header to forward an existing trace ID from an
// upstream system; if absent, a new ID is generated for the request.
const RequestIDHeader = "X-Request-ID"

// RequestID is middleware that ensures every request carries a correlation ID.
// It reads X-Request-ID from the incoming request; if absent it generates a
// cryptographically random 16-byte hex ID. The ID is stored on the request
// context and echoed back in the response header so callers can correlate
// their request with log entries.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		ctx := requestid.NewContext(r.Context(), id)
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext retrieves the request ID from ctx.
// Returns an empty string if none is present.
// Deprecated: call requestid.FromContext directly.
func FromContext(ctx context.Context) string {
	return requestid.FromContext(ctx)
}

// newRequestID generates a random 16-byte hex string.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// rand.Read only fails on catastrophic OS-level entropy failure.
		// Return a static fallback rather than propagating — a missing ID
		// is better than a 500 on every request.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}
