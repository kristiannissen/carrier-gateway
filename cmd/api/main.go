// Package main is the entry point for the API application.
// This file is located at /cmd/api/main.go.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"logistics-gateway/internal/adapter"
	"logistics-gateway/internal/router"
)

// VercelHandler is the Vercel Serverless Function entry point.
// This function is called by Vercel's runtime for each request.
func VercelHandler(w http.ResponseWriter, r *http.Request) {
	// Initialize structured logger
	slog.Info("Handling Vercel request")

	// Initialize carrier adapters
	adapters := initAdapters()

	// Create router
	rtr := router.NewRouter(adapters)

	// Serve the request using the router
	rtr.ServeHTTP(w, r)
}

// initAdapters initializes carrier adapters based on environment variables.
func initAdapters() map[string]adapter.CarrierAdapter {
	adapters := make(map[string]adapter.CarrierAdapter)
	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	mockMode := os.Getenv("MOCK_MODE") == "true"

	if postNordAPIKey != "" && !mockMode {
		adapters["postnord"] = adapter.NewPostNordAdapter(postNordAPIKey)
		slog.Info("PostNord adapter initialized in production mode")
	} else {
		adapters["postnord"] = adapter.NewMockPostNordAdapter()
		slog.Info("PostNord adapter initialized in mock mode")
	}

	return adapters
}

// main is the entry point for local development.
// This function is not used by Vercel but allows local testing with `go run`.
func main() {
	slog.Info("Starting local server on :8080")
	adapters := initAdapters()
	rtr := router.NewRouter(adapters)
	http.ListenAndServe(":8080", rtr)
}
