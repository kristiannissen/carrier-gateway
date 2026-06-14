// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dhl_ecommerce.go.
package adapter

import (
	"context"
)

// MockDHLECSAdapter is a mock implementation of CarrierAdapter and ManifestAdapter
// for DHL eCommerce Americas. It returns canned responses without making network calls.
type MockDHLECSAdapter struct{}

// BookShipment is not yet implemented for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) BookShipment(_ context.Context, _ BookingRequest) (*BookingResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "booking", "not yet implemented")
}

// TrackShipment is not yet implemented for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "tracking", "not yet implemented")
}

// FetchLabel is not yet implemented for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "label fetch", "not yet implemented")
}

// CancelShipment is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "cancellation", "contact DHL customer service")
}

// UpdateShipment is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "post-booking update", "contact DHL customer service")
}

// BookPickup is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) BookPickup(_ context.Context, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup booking", "contact DHL customer service")
}

// UpdatePickup is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup update", "contact DHL customer service")
}

// CancelPickup is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DHL eCommerce Americas", "pickup cancellation", "contact DHL customer service")
}

// GetPickupAvailability is not supported for DHL eCommerce Americas.
func (m *MockDHLECSAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup availability", "")
}

// CloseManifest returns a canned successful manifest response.
func (m *MockDHLECSAdapter) CloseManifest(_ context.Context, req ManifestRequest) (*ManifestResponse, error) {
	return &ManifestResponse{
		Carrier:                "dhl_ecommerce",
		Date:                   req.Date,
		Status:                 "closed",
		ParcelsConfirmed:       len(req.TrackingNumbers),
		ManifestDocument:       mockLabelData,
		ManifestDocumentFormat: "PDF",
		Warnings:               []string{},
	}, nil
}
