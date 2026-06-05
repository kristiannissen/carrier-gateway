// Package main is the entry point for the CLI application.
// This file is located at /cmd/cli/main.go.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/logger"
)

func main() {
	log, err := logger.New()
	if err != nil {
		panic("Failed to initialize logger:" + err.Error())
	}
	defer log.Sync() //nolint:errcheck

	carrierAdapters := adapter.InitAdapters(log)

	rootCmd := &cobra.Command{
		Use:   "logistics-gateway",
		Short: "Multi-Carrier Integration CLI",
		Long:  "A CLI tool for booking shipments and tracking with multiple carriers (PostNord, FedEx, DHL).",
	}

	rootCmd.AddCommand(
		newBookCmd(carrierAdapters),
		newTrackCmd(carrierAdapters),
		newHealthCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
