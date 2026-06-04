// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dao.go.
package adapter

import (
	"fmt"
	"context"
)

// MockDAOAdapter is a mock implementation of the CarrierAdapter interface for DAO.
type MockDAOAdapter struct{}

// BookShipment mocks booking a shipment with DAO.
func (a *MockDAOAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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
		TrackingNumber: "DAO123456789DK",
		LabelURL:       "https://example.com/mock-dao-label.png",
		Carrier:        "dao",
	}, nil
}

// TrackShipment mocks tracking a shipment with DAO.
func (a *MockDAOAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Pakke modtaget på fordelingscenter",
				Location:  "DAO Erritsø",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving parcel shops for DAO.
func (a *MockDAOAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "DAO001",
			Name: "DAO Pakkeshop 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "1234",
				City:       "Copenhagen",
				Country:    "DK",
			},
		},
	}, nil
}
