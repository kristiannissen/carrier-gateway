// Package handler provides the Vercel Serverless Function entry point.
// This file is located at /cmd/api/vercel_handler.go.
package handler

import (
	"log/slog"
	"net/http"

	"logistics-gateway/internal/adapter"
	"logistics-gateway/internal/router"
)

// Handler is the Vercel Serverless Function entry point.
// This function is called by Vercel's runtime for each request.
func Handler(w http.ResponseWriter, r *http.Request) {
	// Initialize structured logger
	slog.Info("Handling Vercel request")

	// Initialize carrier adapters
	adapters := adapter.InitAdapters()

	// Create router
	rtr := router.NewRouter(adapters)

	// Serve the request using the router
	rtr.ServeHTTP(w, r)
}
