// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dao.go.
package adapter

import (
	"context"
	"fmt"
)

// MockDAOAdapter is a mock implementation of the CarrierAdapter interface for DAO.
type MockDAOAdapter struct{}

// BookShipment mocks booking a shipment with DAO.
func (a *MockDAOAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
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
		TrackingNumber: "DAO123456789DK",
		LabelURL:       "https://example.com/mock-dao-label.png",
		Carrier:        "dao",
	}, nil
}

// FetchLabel returns an error — DAO label support is under investigation.
func (a *MockDAOAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, fmt.Errorf("DAO label support is under investigation and not yet available; download labels from the DAO portal")
}

// TrackShipment mocks tracking a shipment with DAO.
func (a *MockDAOAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
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
