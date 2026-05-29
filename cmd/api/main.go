// Package main is the entry point for the API application.
// This file is located at /cmd/api/main.go.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/vercel-go/vercel"

	"logistics-gateway/internal/adapter"
	"logistics-gateway/internal/handler"
)

func main() {
	// Initialize structured logger
	slog.Info("Starting logistics-gateway service")

	// Initialize carrier adapters
	adapters := make(map[string]adapter.CarrierAdapter)

	// Check for PostNord API key
	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	mockMode := os.Getenv("MOCK_MODE") == "true"

	if postNordAPIKey != "" && !mockMode {
		// Use real PostNord adapter
		adapters["postnord"] = adapter.NewPostNordAdapter(postNordAPIKey)
		slog.Info("PostNord adapter initialized in production mode")
	} else {
		// Use mock PostNord adapter
		adapters["postnord"] = adapter.NewMockPostNordAdapter()
		slog.Info("PostNord adapter initialized in mock mode")
	}

	// Create handler config
	handlerConfig := handler.Config{
		Adapters: adapters,
	}

	// Create router
	r := mux.NewRouter()

	// Routes
	r.HandleFunc("/api/bookings", handlerConfig.BookShipment).Methods("POST")
	r.HandleFunc("/api/trackings/{trackingNumber}", handlerConfig.GetTracking).Methods("GET")
	r.HandleFunc("/api/service-points", handlerConfig.GetServicePoints).Methods("GET")
	r.HandleFunc("/api/health", handler.HealthCheck).Methods("GET")

	// Vercel handler
	vc := vercel.New(vercel.Config{
		FunctionTimeout: 30, // Timeout in seconds (default is 5, but PostNord may need longer)
	})

	// Add all routes to Vercel
	vc.HandleFunc("/api/bookings", handlerConfig.BookShipment)
	vc.HandleFunc("/api/trackings/{trackingNumber}", handlerConfig.GetTracking)
	vc.HandleFunc("/api/service-points", handlerConfig.GetServicePoints)
	vc.HandleFunc("/api/health", handler.HealthCheck)

	// Start Vercel server
	slog.Info("Vercel server starting...")
	if err := vc.ListenAndServe(":" + getPort()); err != nil {
		slog.Error("Vercel server failed", "error", err)
		os.Exit(1)
	}
}

// getPort returns the port from environment variables or defaults to 8080.
func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		return "8080"
	}
	return port
}
