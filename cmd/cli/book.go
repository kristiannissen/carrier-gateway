// Package main provides the CLI command for booking shipments.
// This file is located at /cmd/cli/book.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

func newBookCmd(registry *adapter.Registry) *cobra.Command {
	var (
		carrier      string
		inputFile    string
		outputFormat string
		async        bool
	)

	cmd := &cobra.Command{
		Use:   "book",
		Short: "Book a shipment",
		Long:  "Book a shipment with a specified carrier (e.g., postnord, bring, gls).",
		RunE: func(cmd *cobra.Command, args []string) error {
			var request adapter.BookingRequest
			if inputFile != "" {
				data, err := os.ReadFile(inputFile) //nolint:gosec // inputFile is provided by the user via CLI flag, not from untrusted input
				if err != nil {
					return fmt.Errorf("failed to read input file: %w", err)
				}
				if err := json.Unmarshal(data, &request); err != nil {
					return fmt.Errorf("failed to parse input file: %w", err)
				}
			} else {
				stat, _ := os.Stdin.Stat() //nolint:errcheck // stat failure means no pipe, handled by the mode check below
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
						return fmt.Errorf("failed to parse stdin: %w", err)
					}
				} else {
					return fmt.Errorf("no input provided: use --input or pipe JSON to stdin")
				}
			}

			if carrier == "" {
				return fmt.Errorf("carrier is required (use --carrier)")
			}
			request.Carrier = carrier

			if err := validateBookingRequest(&request); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			a, err := registry.Select(carrier)
			if err != nil {
				return fmt.Errorf("unsupported carrier: %w", err)
			}

			response, err := a.BookShipment(cmd.Context(), request)
			if err != nil {
				return fmt.Errorf("booking failed: %w", err)
			}

			if outputFormat == "json" {
				return json.NewEncoder(os.Stdout).Encode(response)
			}

			fmt.Printf("Shipment ID: %s\n", response.ShipmentID)
			fmt.Printf("Tracking Number: %s\n", response.TrackingNumber)
			fmt.Printf("Label URL: %s\n", response.LabelURL)
			fmt.Printf("Carrier: %s\n", response.Carrier)
			fmt.Printf("Cost: %.2f %s\n", response.Cost, response.Currency)
			fmt.Printf("Service Level: %s\n", response.ServiceLevel)
			fmt.Printf("Status: %s\n", response.Status)
			if len(response.Colli) > 0 {
				fmt.Println("\nColli:")
				for _, colli := range response.Colli {
					fmt.Printf("  - ID: %s, Reference: %s, Tracking: %s, Status: %s\n",
						colli.ID, colli.Reference, colli.TrackingNumber, colli.Status)
				}
			}
			if async {
				fmt.Println("\nNote: Async mode enabled. Use 'logistics-gateway track' to check status.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&carrier, "carrier", "c", "", "Carrier (e.g., postnord, bring, gls)")
	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input JSON file (default: stdin)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (json or text)")
	cmd.Flags().BoolVarP(&async, "async", "a", false, "Enable async booking (if supported by carrier)")
	cmd.MarkFlagRequired("carrier") //nolint:errcheck,gosec // cobra flag errors only occur on misconfiguration, not at runtime

	return cmd
}

// validateBookingRequest validates the fields of a BookingRequest before
// forwarding it to a carrier adapter.
func validateBookingRequest(request *adapter.BookingRequest) error {
	if request.Carrier == "" {
		return fmt.Errorf("carrier is required")
	}
	if request.Shipment.Sender.Name == "" || request.Shipment.Sender.Street == "" ||
		request.Shipment.Sender.City == "" || request.Shipment.Sender.Country == "" {
		return fmt.Errorf("sender address is incomplete")
	}
	if request.Shipment.Receiver.Name == "" || request.Shipment.Receiver.Street == "" ||
		request.Shipment.Receiver.City == "" || request.Shipment.Receiver.Country == "" {
		return fmt.Errorf("receiver address is incomplete")
	}
	if len(request.Shipment.Colli) == 0 {
		return fmt.Errorf("shipment must have at least one colli")
	}
	if request.Shipment.TotalWeight <= 0 {
		return fmt.Errorf("total weight must be greater than 0")
	}
	for i, colli := range request.Shipment.Colli {
		if colli.Weight <= 0 {
			return fmt.Errorf("colli %d: weight must be greater than 0", i)
		}
		if len(colli.Items) == 0 {
			return fmt.Errorf("colli %d: must contain at least one item", i)
		}
		for j, item := range colli.Items {
			if item.Weight <= 0 {
				return fmt.Errorf("colli %d, item %d: weight must be greater than 0", i, j)
			}
			if item.Quantity <= 0 {
				return fmt.Errorf("colli %d, item %d: quantity must be greater than 0", i, j)
			}
		}
	}
	var colliTotalWeight float64
	for _, colli := range request.Shipment.Colli {
		colliTotalWeight += colli.Weight
	}
	if colliTotalWeight != request.Shipment.TotalWeight {
		return fmt.Errorf("total weight does not match sum of colli weights (expected %.2f, got %.2f)",
			colliTotalWeight, request.Shipment.TotalWeight)
	}
	return nil
}
