// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_airmee.go.
package adapter

import (
	"context"
	"fmt"
)

// MockAirmeeAdapter is a mock implementation of the CarrierAdapter interface for Airmee.
type MockAirmeeAdapter struct{}

// BookShipment mocks booking a shipment with Airmee.
func (a *MockAirmeeAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}

	var sumColliWeight float64
	for _, colli := range request.Shipment.Colli {
		sumColliWeight += colli.Weight
	}

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
func (a *MockAirmeeAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
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
// Airmee does not have traditional service points, so an empty list is returned.
func (a *MockAirmeeAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	return []ServicePoint{}, nil
}
