// Package main is the entry point for the CLI application.
// This file is located at /cmd/cli/main.go.
package main

import (
	"fmt"
	"os"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/spf13/cobra"
)

func main() {
	// Root command
	rootCmd := &cobra.Command{
		Use:   "logistics-gateway",
		Short: "Multi-Carrier Integration CLI",
		Long:  "A CLI tool for booking shipments and tracking with multiple carriers (PostNord, FedEx, DHL).",
	}

	// Initialize adapters (shared with API)
	adapters := initAdapters()

	// Add subcommands
	rootCmd.AddCommand(
		newBookCmd(adapters),
		newTrackCmd(adapters),
		newServicePointsCmd(adapters),
		newHealthCmd(),
	)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// initAdapters initializes carrier adapters based on environment variables.
// Reuses the same logic as the API.
func initAdapters() map[string]adapter.CarrierAdapter {
	adapters := make(map[string]adapter.CarrierAdapter)
	mockMode := os.Getenv("MOCK_MODE") == "true"

	// PostNord
	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	if postNordAPIKey != "" && !mockMode {
		adapters["postnord"] = adapter.NewPostNordAdapter(postNordAPIKey)
	} else {
		adapters["postnord"] = adapter.NewMockPostNordAdapter()
	}

	return adapters
}
