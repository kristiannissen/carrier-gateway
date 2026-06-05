// Package router provides the HTTP router for the API.
// This file is located at /internal/router/router.go.
package router

import (
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/handler"
	"github.com/kristiannissen/logistics-gateway/internal/middleware"
)

// NewRouter creates and configures the HTTP router for the API.
func NewRouter(adapters map[string]adapter.CarrierAdapter, log *zap.Logger) *mux.Router {
	h := &handler.Config{
		Adapters: adapters,
		Log:      log,
	}

	r := mux.NewRouter()
	r.Use(middleware.RequestID)

	r.HandleFunc("/api/bookings", h.BookShipment).Methods("POST")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.GetTracking).Methods("GET")
	r.HandleFunc("/api/service-points", h.GetServicePoints).Methods("GET")
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	return r
}
