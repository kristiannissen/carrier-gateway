// Package adapter provides a mock Ufficio Postale CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_ufficiopostale.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"

	"go.uber.org/zap"
)

// MockUfficioPostaleAdapter implements CarrierAdapter with pre-canned Ufficio Postale responses.
// BookShipmentFunc and TrackShipmentFunc can be overridden to inject custom
// responses or errors in tests:
//
//	adapter := &MockUfficioPostaleAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockUfficioPostaleAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// NewMockUfficioPostaleAdapter returns a MockUfficioPostaleAdapter with default behaviour.
func NewMockUfficioPostaleAdapter() *MockUfficioPostaleAdapter {
	return &MockUfficioPostaleAdapter{}
}

// BookShipment returns a mock Ufficio Postale booking response.
func (m *MockUfficioPostaleAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	zap.L().Info("MockUfficioPostaleAdapter: returning mock booking response")

	// Raccomandate tracking numbers are typically 12-digit numerics.
	trackingNumber := fmt.Sprintf("%012d", rand.Intn(999999999999)) //nolint:gosec // mock data
	internalID := fmt.Sprintf("%024x", rand.Uint64())               //nolint:gosec // mock data

	return &BookingResponse{
		ShipmentID:     internalID,
		TrackingNumber: trackingNumber,
		Carrier:        "ufficiopostale",
		Status:         "confirmed",
		BetaWarning:    "Ufficio Postale adapter is in beta — label fetch, cancellation, and update are not supported",
	}, nil
}

// TrackShipment returns a mock tracking response.
func (m *MockUfficioPostaleAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	if trackingNumber == "" {
		return nil, fmt.Errorf("ufficiopostale: tracking number must not be empty")
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "ufficiopostale",
		Status:           "Accettato Online",
		NormalizedStatus: StatusBooked,
		OriginalStatus:   "Accettato Online",
		Events: []TrackingEvent{
			{
				Timestamp:        "2024-01-01T09:00:00Z",
				Status:           "Accettato Online",
				NormalizedStatus: StatusBooked,
				Details:          "Accettato Online",
			},
		},
	}, nil
}

// FetchLabel returns unsupported for Ufficio Postale — no label endpoint exists.
func (m *MockUfficioPostaleAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("Ufficio Postale", "label fetch",
		"Poste Italiane prints and dispatches the letter internally; no carrier label is issued")
}

// CancelShipment returns unsupported for Ufficio Postale — no cancellation endpoint exists.
func (m *MockUfficioPostaleAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Ufficio Postale", "shipment cancellation",
		"the Ufficio Postale API has no cancellation endpoint")
}

// UpdateShipment returns unsupported for Ufficio Postale — PATCH accepts only confirmed boolean.
func (m *MockUfficioPostaleAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Ufficio Postale", "post-booking update",
		"the Ufficio Postale API does not support updating a shipment after creation")
}
