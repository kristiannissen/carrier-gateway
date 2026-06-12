// Package main is the general entry point for the API application.
// This file is located at /cmd/api/main.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/logger"
	"github.com/kristiannissen/carrier-gateway/internal/notification"
	"github.com/kristiannissen/carrier-gateway/internal/router"
)

func main() {
	log, err := logger.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	log.Info("starting logistics-gateway API server")

	registry := adapter.NewRegistry(log)
	notifSvc := notification.NewService(notification.NewHTTPSender(), log)
	rtr := router.NewRouter(registry, notifSvc, log)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// SHUTDOWN_TIMEOUT controls how long the server waits for in-flight
	// requests to complete after receiving SIGTERM. Docker's default
	// stop-timeout is 10 s; set this lower so the drain completes before
	// Docker sends SIGKILL.
	shutdownTimeout := 8 * time.Second
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		if d, parseErr := time.ParseDuration(v); parseErr == nil {
			shutdownTimeout = d
		}
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: rtr,
		// Defend against slow-loris and large-header attacks.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start the server in a goroutine so we can listen for signals below.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("server listening", zap.String("port", port))
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Block until SIGTERM or SIGINT (Ctrl-C in local dev).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErr:
		log.Fatal("server failed", zap.Error(err))
	case sig := <-quit:
		log.Info("shutdown signal received", zap.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", zap.Error(err))
	} else {
		log.Info("server stopped cleanly")
	}
}
