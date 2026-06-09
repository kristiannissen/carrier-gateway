// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_posti.go.
package adapter

import (
	"context"
	"fmt"
)

// MockPostiAdapter is a mock implementation of the CarrierAdapter interface for Posti.
type MockPostiAdapter struct{}

// BookShipment mocks booking a shipment with Posti.
func (a *MockPostiAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
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
		TrackingNumber: "POSTI123456789FI",
		LabelURL:       "https://example.com/mock-posti-label.png",
		Carrier:        "posti",
	}, nil
}

// FetchLabel returns a mock label response for Posti.
func (a *MockPostiAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "posti",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for Posti.
func (a *MockPostiAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, fmt.Errorf("posti does not support cancellation via this gateway")
}

// UpdateShipment returns unsupported for Posti.
func (a *MockPostiAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, fmt.Errorf("posti does not support post-booking updates via this gateway")
}

// TrackShipment mocks tracking a shipment with Posti.
func (a *MockPostiAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "posti",
		Status:           "In Transit",
		NormalizedStatus: StatusUnknown,
		OriginalStatus:   "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp:        "2026-05-31T12:00:00Z",
				Status:           "Shipment Accepted",
				NormalizedStatus: StatusUnknown,
				Location:         "Helsinki",
			},
		},
	}, nil
}
