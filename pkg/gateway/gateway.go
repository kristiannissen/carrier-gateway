// Package gateway exposes a public entry point for the logistics-gateway application.
package gateway

import (
	"net/http"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/logger"
	"github.com/kristiannissen/logistics-gateway/internal/router"
)

// NewHandler initialises the application and returns an http.Handler.
func NewHandler() http.Handler {
	log, err := logger.New()
	if err != nil {
		panic("Failed to initialize logger:" + err.Error())
	}
	defer log.Sync()

	adapters := adapter.InitAdapters(log)
	return router.NewRouter(adapters)
}
