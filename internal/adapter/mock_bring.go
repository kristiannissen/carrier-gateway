// Package adapter provides a mock Bring CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_bring.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockBringAdapter implements CarrierAdapter with pre-canned Bring responses.
// All three methods can be overridden via their corresponding Func fields:
//
//	adapter := &MockBringAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockBringAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock Bring booking response, applying the same
// validation as the real BringAdapter so tests catch input errors without a live API.
func (m *MockBringAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}

	var sum float64
	for _, c := range request.Shipment.Colli {
		sum += c.Weight
	}
	if request.Shipment.TotalWeight != sum {
		return nil, fmt.Errorf("TotalWeight must match the sum of all colli weights")
	}

	zap.L().Info("MockBringAdapter: returning mock booking response")

	consignment := fmt.Sprintf("BR%09dNO", rand.Intn(1000000000)) //nolint:gosec // mock data, not security-sensitive

	return &BookingResponse{
		TrackingNumber: consignment,
		LabelURL:       fmt.Sprintf("https://mock.bring.com/labels/%s.pdf", consignment),
		Carrier:        "bring",
		Cost:           125.50,
		Currency:       "NOK",
		ServiceLevel:   "Standard",
		Status:         "booked",
	}, nil
}

// TrackShipment returns a mock Bring tracking response with three canned events.
func (m *MockBringAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockBringAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "HANDED_IN",
			NormalizedStatus: StatusPickedUp,
			Location:         "Oslo, NO",
		},
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "IN_TRANSIT",
			NormalizedStatus: StatusInTransit,
			Location:         "Gothenburg, SE",
		},
		{
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			Status:           "DELIVERED",
			NormalizedStatus: StatusDelivered,
			Location:         "Stockholm, SE",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "bring",
		Status:            "Delivered",
		NormalizedStatus:  StatusDelivered,
		OriginalStatus:    "DELIVERED",
		EstimatedDelivery: time.Now().UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}

// FetchLabel returns a mock label response.
func (m *MockBringAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "bring",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a mock cancel response for Bring.
func (m *MockBringAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	return &CancelResponse{TrackingNumber: trackingNumber, Carrier: "bring", Status: "cancelled"}, nil
}

// UpdateShipment returns unsupported for Bring.
func (m *MockBringAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Bring", "post-booking update", "")
}

// BookPickup returns a canned pickup confirmation for Bring.
func (m *MockBringAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	return &PickupResponse{
		Carrier:            "bring",
		ConfirmationNumber: "MOCK-BRING-PICKUP-001",
		Date:               req.Pickup.Date,
		ReadyTime:          "08:00",
		CloseTime:          "16:00",
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for Bring.
func (m *MockBringAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Bring", "pickup update", "cancel and rebook via BookPickup")
}

// CancelPickup is not supported for Bring.
func (m *MockBringAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("Bring", "pickup cancellation", "contact Bring customer service to cancel")
}

// CloseManifest is not supported for Bring.
func (m *MockBringAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Bring", "manifest close", "")
}

// GetPickupAvailability is not supported for Bring.
func (m *MockBringAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Bring", "pickup availability", "")
}

// RegisterCustomerWebhook returns a canned customer webhook subscription for Bring.
func (m *MockBringAdapter) RegisterCustomerWebhook(_ context.Context, req BringCustomerWebhookRequest) (*BringCustomerWebhookResponse, error) {
	if req.CustomerNumber == "" {
		return nil, fmt.Errorf("customerNumber is required")
	}
	if len(req.EventSet) == 0 {
		return nil, fmt.Errorf("eventSet must contain at least one event")
	}
	if req.WebhookConfiguration.WebhookURL == "" {
		return nil, fmt.Errorf("webhookConfiguration.webhookUrl is required")
	}
	return &BringCustomerWebhookResponse{
		ID:                   "mock-subscription-id",
		CustomerNumber:       req.CustomerNumber,
		EventSet:             req.EventSet,
		WebhookConfiguration: req.WebhookConfiguration,
		Created:              time.Now().UTC(),
		Expiry:               time.Now().Add(365 * 24 * time.Hour).UTC(),
	}, nil
}

// DeleteCustomerWebhook is a no-op for the mock.
func (m *MockBringAdapter) DeleteCustomerWebhook(_ context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return fmt.Errorf("subscriptionID is required")
	}
	return nil
}

// RenewCustomerWebhook returns a canned renewed subscription for the mock.
func (m *MockBringAdapter) RenewCustomerWebhook(_ context.Context, subscriptionID string) (*BringCustomerWebhookResponse, error) {
	if subscriptionID == "" {
		return nil, fmt.Errorf("subscriptionID is required")
	}
	return &BringCustomerWebhookResponse{
		ID:      subscriptionID,
		Created: time.Now().UTC(),
		Expiry:  time.Now().Add(365 * 24 * time.Hour).UTC(),
	}, nil
}

// NewMockBringAdapter returns a new MockBringAdapter with default behaviour.
func NewMockBringAdapter() *MockBringAdapter {
	return &MockBringAdapter{}
}
