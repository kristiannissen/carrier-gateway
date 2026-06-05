// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/idempotency.go.
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"go.uber.org/zap"
)

// idempotencyKeyCtxKey is an unexported context key for the idempotency key.
type idempotencyKeyCtxKey struct{}

// IdempotencyHeader is the HTTP header name for the idempotency key.
const IdempotencyHeader = "Idempotency-Key"

// maxIdempotencyHeaderLen is the maximum length of the Idempotency-Key header
// value. Matches the limit enforced by the validation package.
const maxIdempotencyHeaderLen = 64

// IdempotencyKeyFromContext retrieves the idempotency key stored on ctx by the
// Idempotency middleware. Returns an empty string if none is present.
func IdempotencyKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyCtxKey{}).(string)
	return key
}

// Idempotency is middleware that reads the Idempotency-Key header and bridges
// it into the request body so that downstream handlers and adapters only need
// to read from BookingRequest.IdempotencyKey.
//
// Rules:
//   - If neither header nor body key is present the request passes through
//     unchanged — idempotency keys are optional.
//   - If the header is present but the body does not contain idempotencyKey,
//     the key is injected into the body JSON.
//   - If both are present they must match; a mismatch returns 422.
//   - A header value longer than 64 characters returns 400.
//   - The resolved key is stored on the request context so other middleware
//     (e.g. LogPayloads) can include it in structured log fields.
func Idempotency(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only POST requests carry a booking body.
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			headerKey := r.Header.Get(IdempotencyHeader)

			if headerKey == "" {
				// No header — pass through; body key (if any) is handled by
				// the validation layer downstream.
				next.ServeHTTP(w, r)
				return
			}

			if len(headerKey) > maxIdempotencyHeaderLen {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "invalid Idempotency-Key",
					"details": "Idempotency-Key header must be 64 characters or fewer",
				})
				return
			}

			// Read the body so we can inspect and potentially patch it.
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Attempt to parse the body as JSON to look for an existing key.
			var body map[string]interface{}
			if jsonErr := json.Unmarshal(raw, &body); jsonErr != nil {
				// Not JSON — restore the body and let the handler deal with it.
				r.Body = io.NopCloser(bytes.NewReader(raw))
				ctx := context.WithValue(r.Context(), idempotencyKeyCtxKey{}, headerKey)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			bodyKey, _ := body["idempotencyKey"].(string)

			switch {
			case bodyKey == "":
				// Header present, body absent — inject header value into body.
				body["idempotencyKey"] = headerKey
				patched, marshalErr := json.Marshal(body)
				if marshalErr != nil {
					log.Error("failed to patch idempotency key into body", zap.Error(marshalErr))
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(patched))
				r.ContentLength = int64(len(patched))

			case bodyKey == headerKey:
				// Both present and matching — restore body unchanged.
				r.Body = io.NopCloser(bytes.NewReader(raw))

			default:
				// Both present but mismatched — reject.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "idempotency key conflict",
					"details": "Idempotency-Key header and idempotencyKey body field must match when both are provided",
				})
				return
			}

			ctx := context.WithValue(r.Context(), idempotencyKeyCtxKey{}, headerKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
