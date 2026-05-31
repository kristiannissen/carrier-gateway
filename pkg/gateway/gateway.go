// Package gateway exposes a public entry point for the logistics-gateway application.
package gateway

import (
	"net/http"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/router"
)

// NewHandler initialises the application and returns an http.Handler.
func NewHandler() http.Handler {
	adapters := adapter.initAdapters()
	return router.NewRouter(adapters)
}
