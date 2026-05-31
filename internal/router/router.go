// Package router provides the HTTP router for the API.
// This file is located at /internal/router/router.go.
package router

import (
	"github.com/gorilla/mux"
	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/handler"
)

// NewRouter creates and configures the HTTP router for the API.
func NewRouter(adapters map[string]adapter.CarrierAdapter) *mux.Router {
	handlerConfig := handler.Config{
		Adapters: adapters,
	}

	r := mux.NewRouter()

	// Routes
	r.HandleFunc("/api/bookings", handlerConfig.BookShipment).Methods("POST")
	r.HandleFunc("/api/trackings/{trackingNumber}", handlerConfig.GetTracking).Methods("GET")
	r.HandleFunc("/api/service-points", handlerConfig.GetServicePoints).Methods("GET")
	r.HandleFunc("/api/health", handler.HealthCheck).Methods("GET")

	return r
}
