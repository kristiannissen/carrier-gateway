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
	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// NewRouter creates and configures the HTTP router for the API.
// notifSvc may be nil when the notification feature is not configured;
// the relevant endpoints will return 503 in that case.
func NewRouter(registry *adapter.Registry, notifSvc *notification.Service, log *zap.Logger) *mux.Router {
	h := &handler.Config{
		Registry:            registry,
		Log:                 log,
		MockMode:            os.Getenv("MOCK_MODE") == "true",
		NotificationService: notifSvc,
	}

	r := mux.NewRouter()
	// Security headers and CORS are intentionally omitted here.
	// This service runs behind a reverse proxy (Traefik/nginx) in Docker; those
	// headers belong at the proxy layer. CORS is browser-only and irrelevant
	// for a server-to-server API.
	r.Use(middleware.RequestID)
	r.Use(middleware.Idempotency(log))
	r.Use(middleware.LogPayloads(log))

	r.HandleFunc("/api/bookings", h.BookShipment).Methods("POST")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.CancelShipment).Methods("DELETE")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.UpdateShipment).Methods("PATCH")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.GetTracking).Methods("GET")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.TrackAndNotify).Methods("POST")
	r.HandleFunc("/api/labels/{trackingNumber}", h.GetLabel).Methods("GET")
	r.HandleFunc("/api/notifications", h.SendNotification).Methods("POST")
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	return r
}
