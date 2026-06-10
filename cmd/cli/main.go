// Package main is the entry point for the CLI application.
// This file is located at /cmd/cli/main.go.
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
		panic("Failed to initialize logger:" + err.Error())
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
		fmt.Println(err)
		os.Exit(1)
	}
}
