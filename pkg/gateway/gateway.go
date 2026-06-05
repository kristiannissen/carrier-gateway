// Package gateway exposes a public entry point for the logistics-gateway application.
// This file is located at /pkg/gateway/gateway.go.
package gateway

import (
	"net/http"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/logger"
	"github.com/kristiannissen/logistics-gateway/internal/router"
)

// NewHandler initialises the application and returns an http.Handler.
// The logger is intentionally not synced here: the returned handler is
// long-lived (served by a Vercel runtime), so flushing on return would
// discard buffered log entries before any request is processed. Callers
// that need a clean shutdown should sync the logger themselves.
func NewHandler() http.Handler {
	log, err := logger.New()
	if err != nil {
		panic("failed to initialise logger: " + err.Error())
	}

	registry := adapter.NewRegistry(log)
	return router.NewRouter(registry, log)
}
