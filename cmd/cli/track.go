// Package main provides the CLI command for tracking shipments.
// This file is located at /cmd/cli/track.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

func newTrackCmd(adapters map[string]adapter.CarrierAdapter) *cobra.Command {
	var (
		trackingNumber string
		carrier        string
		outputFormat   string
	)

	cmd := &cobra.Command{
		Use:   "track",
		Short: "Track a shipment",
		Long:  "Track a shipment by tracking number.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if trackingNumber == "" {
				return fmt.Errorf("tracking number is required (use --tracking-number)")
			}
			if carrier == "" {
				carrier = "postnord"
			}

			a, exists := adapters[carrier]
			if !exists {
				return fmt.Errorf("unsupported carrier: %s", carrier)
			}

			response, err := a.TrackShipment(cmd.Context(), trackingNumber)
			if err != nil {
				return fmt.Errorf("tracking failed: %w", err)
			}

			if outputFormat == "json" {
				return json.NewEncoder(os.Stdout).Encode(response)
			}

			fmt.Printf("Shipment ID: %s\n", response.ShipmentID)
			fmt.Printf("Tracking Number: %s\n", response.TrackingNumber)
			fmt.Printf("Carrier: %s\n", response.Carrier)
			fmt.Printf("Status: %s\n", response.Status)
			if response.EstimatedDelivery != "" {
				fmt.Printf("Estimated Delivery: %s\n", response.EstimatedDelivery)
			}
			if len(response.Events) > 0 {
				fmt.Println("\nEvents:")
				for _, event := range response.Events {
					fmt.Printf("  - %s: %s (%s)\n", event.Timestamp, event.Status, event.Location)
					if event.Details != "" {
						fmt.Printf("    Details: %s\n", event.Details)
					}
				}
			}
			if len(response.Colli) > 0 {
				fmt.Println("\nColli:")
				for _, colli := range response.Colli {
					fmt.Printf("  - ID: %s, Tracking: %s, Status: %s\n",
						colli.ID, colli.TrackingNumber, colli.Status)
					if len(colli.Events) > 0 {
						fmt.Println("    Events:")
						for _, event := range colli.Events {
							fmt.Printf("      - %s: %s (%s)\n", event.Timestamp, event.Status, event.Location)
						}
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&trackingNumber, "tracking-number", "t", "", "Tracking number")
	cmd.Flags().StringVarP(&carrier, "carrier", "c", "postnord", "Carrier (e.g., postnord, fedex, dhl)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (json or text)")
	cmd.MarkFlagRequired("tracking-number") //nolint:errcheck

	return cmd
}
