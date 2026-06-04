// Package main is the general entry point for the API application.
// This file is located at /cmd/api/main.go.
package main

import (
	"net/http"
	"os"

	"go.uber.org/zap"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/logger"
	"github.com/kristiannissen/logistics-gateway/internal/router"
)

func main() {
	log, err := logger.New()
	if err != nil {
		panic("failed to initialise logger: " + err.Error())
	}
	defer log.Sync() //nolint:errcheck

	log.Info("starting logistics-gateway API server")

	adapters := adapter.InitAdapters(log)
	rtr := router.NewRouter(adapters, log)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Info("server listening", zap.String("port", port))
	if err := http.ListenAndServe(":"+port, rtr); err != nil {
		log.Fatal("server failed", zap.Error(err))
	}
}
