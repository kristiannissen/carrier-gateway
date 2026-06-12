// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/idempotency.go.
package validation

import "fmt"

// MaxIdempotencyKeyLength is the maximum number of characters allowed in an
// idempotency key. Keys longer than this are rejected to avoid abuse and to
// stay within carrier API header length limits. Exported so the middleware
// layer can enforce the same limit without duplicating the value.
const MaxIdempotencyKeyLength = 64

// ValidateIdempotencyKey validates the idempotency key if one is provided.
// A missing key is valid — the request is processed normally with no
// deduplication. Duplicate key detection is stateful and handled separately.
func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return nil
	}
	if len(key) > MaxIdempotencyKeyLength {
		return fmt.Errorf(
			"idempotency key must be %d characters or fewer (got %d)",
			MaxIdempotencyKeyLength, len(key),
		)
	}
	return nil
}
