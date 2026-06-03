// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_airmee.go.
package adapter

import "fmt"

// MockAirmeeAdapter is a mock implementation of the CarrierAdapter interface for Airmee.
type MockAirmeeAdapter struct{}

// BookShipment mocks booking a shipment with Airmee.
func (a *MockAirmeeAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	// Validate TotalWeight is provided
	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}

	// Calculate sum of all colli weights
	var sumColliWeight float64
	for _, colli := range request.Shipment.Colli {
		sumColliWeight += colli.Weight
	}

	// Validate TotalWeight matches sum of colli weights
	if request.Shipment.TotalWeight != sumColliWeight {
		return nil, fmt.Errorf("TotalWeight must match the sum of all colli weights")
	}

	return &BookingResponse{
		TrackingNumber: "AIRMEE123456789",
		LabelURL:       "https://example.com/mock-airmee-tracking",
		Carrier:        "airmee",
	}, nil
}

// TrackShipment mocks tracking a shipment with Airmee.
func (a *MockAirmeeAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Courier picked up the parcel",
				Location:  "Copenhagen",
			},
			{
				Timestamp: "2026-05-31T13:00:00Z",
				Status:    "Courier is on the way to delivery",
				Location:  "Copenhagen",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving service points for Airmee.
func (a *MockAirmeeAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	// Airmee does not have traditional service points, so return an empty list
	return []ServicePoint{}, nil
}
