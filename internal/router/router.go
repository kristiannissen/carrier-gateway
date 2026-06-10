// Package router provides the HTTP router for the API.
// This file is located at /internal/router/router.go.
package router

import (
	"os"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/handler"
	"github.com/kristiannissen/carrier-gateway/internal/middleware"
)

// NewRouter creates and configures the HTTP router for the API.
func NewRouter(registry *adapter.Registry, log *zap.Logger) *mux.Router {
	h := &handler.Config{
		Registry: registry,
		Log:      log,
		MockMode: os.Getenv("MOCK_MODE") == "true",
	}

	r := mux.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Idempotency(log))
	r.Use(middleware.LogPayloads(log))

	r.HandleFunc("/api/bookings", h.BookShipment).Methods("POST")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.CancelShipment).Methods("DELETE")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.UpdateShipment).Methods("PATCH")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.GetTracking).Methods("GET")
	r.HandleFunc("/api/labels/{trackingNumber}", h.GetLabel).Methods("GET")
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	return r
}
