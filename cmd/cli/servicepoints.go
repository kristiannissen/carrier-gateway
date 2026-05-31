// Package main provides the CLI command for retrieving service points.
// This file is located at /cmd/cli/servicepoints.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/spf13/cobra"
)

func newServicePointsCmd(adapters map[string]adapter.CarrierAdapter) *cobra.Command {
	var (
		city         string
		postalCode   string
		country      string
		carrier      string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "service-points",
		Short: "Get service points",
		Long:  "Retrieve service points (e.g., pickup locations) for a carrier.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if city == "" || country == "" {
				return fmt.Errorf("city and country are required (use --city and --country)")
			}
			if carrier == "" {
				carrier = "postnord" // Default to PostNord
			}

			// Get adapter
			adapter, exists := adapters[carrier]
			if !exists {
				return fmt.Errorf("unsupported carrier: %s", carrier)
			}

			// Create location
			location := adapter.Location{
				City:       city,
				PostalCode: postalCode,
				Country:    country,
			}

			// Get service points
			servicePoints, err := adapter.GetServicePoints(location)
			if err != nil {
				return fmt.Errorf("failed to get service points: %v", err)
			}

			// Output response
			if outputFormat == "json" {
				return json.NewEncoder(os.Stdout).Encode(servicePoints)
			} else {
				fmt.Printf("Service Points for %s, %s (%s):\n\n", city, postalCode, country)
				for i, sp := range servicePoints {
					fmt.Printf("%d. %s (ID: %s)\n", i+1, sp.Name, sp.ID)
					fmt.Printf("   Address: %s, %s, %s %s\n",
						sp.Address.Street, sp.Address.City, sp.Address.PostalCode, sp.Address.Country)
					if sp.OpeningHours != "" {
						fmt.Printf("   Opening Hours: %s\n", sp.OpeningHours)
					}
					if len(sp.Services) > 0 {
						fmt.Printf("   Services: %v\n", sp.Services)
					}
					fmt.Println()
				}
			}
			return nil
		},
	}

	// Flags
	cmd.Flags().StringVarP(&city, "city", "", "", "City")
	cmd.Flags().StringVarP(&postalCode, "postal-code", "", "", "Postal code (optional)")
	cmd.Flags().StringVarP(&country, "country", "", "", "Country code (e.g., DK)")
	cmd.Flags().StringVarP(&carrier, "carrier", "c", "postnord", "Carrier (e.g., postnord, fedex, dhl)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (json or text)")

	// Mark required flags
	cmd.MarkFlagRequired("city")
	cmd.MarkFlagRequired("country")

	return cmd
}
