// Package gateway exposes a public entry point for the logistics-gateway application.
// This allows external callers (e.g. Vercel serverless functions) to use the application
// without importing internal packages directly.
package gateway

import (
    "net/http"

    "github.com/kristiannissen/logistics-gateway/internal/adapter"
    "github.com/kristiannissen/logistics-gateway/internal/router"
)

// NewHandler initialises the application and returns an http.Handler.
func NewHandler() http.Handler {
    adapters := adapter.InitAdapters()
    return router.NewRouter(adapters)
}
