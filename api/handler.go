// Package handler provides the Vercel Serverless Function entry point.
// This file is located at /vercel/handler.go.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/kristiannissen/logistics-gateway/pkg/gateway"
)

// Handler is the Vercel Serverless Function entry point.
// This function is called by Vercel's runtime for each request.
func Handler(w http.ResponseWriter, r *http.Request) {
	// Initialize structured logger
	slog.Info("Handling Vercel request " + r.URL.Path)

	gateway.NewHandler().ServeHTTP(w, r)
}
