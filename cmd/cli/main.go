// Package main is the entry point for the CLI application.
// This file is located at /cmd/cli/main.go.
//
// Logging policy: structured zap logging is used for internal diagnostics.
// Human-readable terminal output (booking results, tracking events, health
// status) is written via fmt.Print* — this is intentional. CLI commands
// produce user-facing text on stdout; errors go to stderr via fmt.Fprintln.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/logger"
)

func main() {
	log, err := logger.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	registry := adapter.NewRegistry(log)

	rootCmd := &cobra.Command{
		Use:   "logistics-gateway",
		Short: "Multi-Carrier Integration CLI",
		Long:  "A CLI tool for booking shipments and tracking with multiple carriers.",
	}

	rootCmd.AddCommand(
		newBookCmd(registry),
		newTrackCmd(registry),
		newHealthCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
