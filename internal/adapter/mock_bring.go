// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_bring.go.
package adapter

import "fmt"

// MockBringAdapter is a mock implementation of the CarrierAdapter interface for Bring.
type MockBringAdapter struct{}

// BookShipment mocks booking a shipment with Bring.
func (a *MockBringAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
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
		TrackingNumber: "BR123456789NO",
		LabelURL:       "https://example.com/mock-bring-label.png",
		Carrier:        "bring",
	}, nil
}

// TrackShipment mocks tracking a shipment with Bring.
func (a *MockBringAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Shipment Accepted",
				Location:  "Oslo",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving service points for Bring.
func (a *MockBringAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "BR001",
			Name: "Bring Service Point 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "0123",
				City:       "Oslo",
				Country:    "NO",
			},
		},
	}, nil
}
