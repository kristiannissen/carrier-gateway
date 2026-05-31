// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/test_helpers.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
)

// NewMockServer creates a new mock HTTP server with the given handler.
func NewMockServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}
