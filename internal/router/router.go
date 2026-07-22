// Package router provides the HTTP router for the API.
// This file is located at /internal/router/router.go.
package router

import (
	"os"
	"strings"

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
	// Security headers and CORS are intentionally omitted here — CORS is
	// browser-only and irrelevant for a server-to-server API. If this
	// service runs behind a reverse proxy or load balancer, security headers
	// and TLS termination belong there. If it is deployed as a bare
	// container with no such layer in front of it, set API_KEYS (below) so
	// the gateway enforces its own access control instead of relying on an
	// assumption about the deployment topology.
	r.Use(middleware.RequestID)
	r.Use(middleware.APIKeyAuth(apiKeys(), log))
	r.Use(middleware.Idempotency(log))
	r.Use(middleware.LogPayloads(log))

	r.HandleFunc("/api/bookings", h.BookShipment).Methods("POST")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.CancelShipment).Methods("DELETE")
	r.HandleFunc("/api/bookings/{trackingNumber}", h.UpdateShipment).Methods("PATCH")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.GetTracking).Methods("GET")
	r.HandleFunc("/api/trackings/{trackingNumber}", h.TrackAndNotify).Methods("POST")
	r.HandleFunc("/api/labels/{trackingNumber}", h.GetLabel).Methods("GET")
	r.HandleFunc("/api/notifications", h.SendNotification).Methods("POST")
	r.HandleFunc("/api/pickups/availability", h.GetPickupAvailability).Methods("GET")
	r.HandleFunc("/api/pickups/cutoff-time", h.GetCutoffTime).Methods("GET")
	r.HandleFunc("/api/pickups", h.BookPickup).Methods("POST")
	r.HandleFunc("/api/pickups", h.ListPickups).Methods("GET")
	r.HandleFunc("/api/pickups/{confirmationNumber}", h.GetPickup).Methods("GET")
	r.HandleFunc("/api/pickups/{confirmationNumber}", h.UpdatePickup).Methods("PUT")
	r.HandleFunc("/api/pickups/{confirmationNumber}", h.CancelPickup).Methods("DELETE")
	r.HandleFunc("/api/manifests", h.CloseManifest).Methods("POST")
	r.HandleFunc("/api/returns", h.BookReturn).Methods("POST")
	r.HandleFunc("/api/returns/{id}", h.GetReturnShipment).Methods("GET")
	r.HandleFunc("/api/returns/{trackingNumber}/label", h.GetReturnLabel).Methods("GET")
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	// Built-in documentation — no auth required, no middleware side-effects.
	// GET /docs              → endpoint index + freight terminology glossary
	// GET /docs/{slug}       → full docs for one endpoint (e.g. /docs/bookings)
	// GET /docs/terminology  → freight glossary only
	r.HandleFunc("/docs", h.DocsIndex).Methods("GET")
	r.HandleFunc("/docs/{slug}", h.DocsEndpoint).Methods("GET")

	return r
}

// apiKeys reads API_KEYS from the environment as a comma-separated list and
// returns the non-empty, trimmed values. An empty result disables API key
// authentication (see middleware.APIKeyAuth).
func apiKeys() []string {
	raw := os.Getenv("API_KEYS")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		if k := strings.TrimSpace(p); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
