// Package main provides the CLI command for health checks.
// This file is located at /cmd/cli/health.go.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Health check",
		Long:  "Check the health of the CLI tool.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("CLI is healthy!")
			fmt.Println("Available carriers: postnord, fedex, dhl")
			fmt.Println("Available commands: book, track, service-points, health")
		},
	}
}
