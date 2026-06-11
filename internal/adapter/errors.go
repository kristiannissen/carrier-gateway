// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/errors.go.
package adapter

import (
	"errors"
	"fmt"
)

// ErrNotSupported is the sentinel error returned when a carrier does not
// support a given operation via its API. Use errors.Is to detect it:
//
//	if errors.Is(err, adapter.ErrNotSupported) { ... }
var ErrNotSupported = errors.New("operation not supported by carrier")

// NotSupportedError describes a carrier operation that is not available via API.
// It wraps ErrNotSupported so errors.Is works transparently.
type NotSupportedError struct {
	// Carrier is the carrier name (e.g. "DHL", "GLS").
	Carrier string
	// Operation is a short description of the unsupported operation (e.g. "cancellation").
	Operation string
	// Reason is an optional human-readable hint (e.g. "contact DHL customer service").
	Reason string
}

// Error implements the error interface.
func (e *NotSupportedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s does not support %s: %s", e.Carrier, e.Operation, e.Reason)
	}
	return fmt.Sprintf("%s does not support %s", e.Carrier, e.Operation)
}

// Unwrap returns ErrNotSupported so errors.Is(err, adapter.ErrNotSupported) works.
func (e *NotSupportedError) Unwrap() error { return ErrNotSupported }

// notSupported constructs a NotSupportedError. reason may be empty.
func notSupported(carrier, operation, reason string) error {
	return &NotSupportedError{Carrier: carrier, Operation: operation, Reason: reason}
}
