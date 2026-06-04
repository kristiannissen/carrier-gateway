// Package handler provides the Vercel Serverless Function entry point.
// This file is located at /api/handler.go.
package handler

import (
	"net/http"

	"github.com/kristiannissen/logistics-gateway/internal/logger"
	"github.com/kristiannissen/logistics-gateway/pkg/gateway"
	"go.uber.org/zap"
)

// Handler is the Vercel Serverless Function entry point.
// This function is called by Vercel's runtime for each request.
func Handler(w http.ResponseWriter, r *http.Request) {
	log, err := logger.New()
	if err != nil {
		// Logger construction failed — fall back to a no-op so the request
		// can still be served; the panic would kill the Vercel invocation.
		log = zap.NewNop()
	}

	log.Info("handling Vercel request", zap.String("path", r.URL.Path))

	gateway.NewHandler().ServeHTTP(w, r)
}
