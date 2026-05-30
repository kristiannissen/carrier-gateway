// Package main is the general entry point for the API application.
// This file is located at /cmd/api/main.go.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"logistics-gateway/internal/adapter"
	"logistics-gateway/internal/router"
)

// main is the entry point for the API application.
// Use this for local development or non-Vercel deployments.
func main() {
	slog.Info("Starting logistics-gateway API server")

	// Initialize carrier adapters
	adapters := adapter.InitAdapters()

	// Create router
	rtr := router.NewRouter(adapters)

	// Start HTTP server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("Server listening on port " + port)
	if err := http.ListenAndServe(":"+port, rtr); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
