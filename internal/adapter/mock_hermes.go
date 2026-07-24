// Package adapter provides a mock Hermes CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_hermes.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"go.uber.org/zap"
)

// MockHermesAdapter implements CarrierAdapter with pre-canned Hermes responses.
// BookShipmentFunc and TrackShipmentFunc can be overridden to inject errors
// or custom responses in tests:
//
//	adapter := &MockHermesAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockHermesAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock Hermes booking response.
func (m *MockHermesAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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

	zap.L().Info("MockHermesAdapter: returning mock booking response")

	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		shipmentID := fmt.Sprintf("H%019d", rand.Intn(1000000000)) //nolint:gosec // mock data
		orderID := fmt.Sprintf("%011d", rand.Intn(100000000000))   //nolint:gosec // mock data
		return &BookingResponse{
			ShipmentID:     orderID,
			TrackingNumber: shipmentID,
			Carrier:        "hermes",
			Status:         "booked",
		}, nil
	}

	shipmentID := fmt.Sprintf("H%019d", rand.Intn(1000000000)) //nolint:gosec // mock data
	orderID := fmt.Sprintf("%011d", rand.Intn(100000000000))   //nolint:gosec // mock data

	return &BookingResponse{
		ShipmentID:     orderID,
		TrackingNumber: shipmentID,
		Carrier:        "hermes",
		Status:         "booked",
		Colli: []ColliResponse{{
			ID:             shipmentID,
			TrackingNumber: shipmentID,
			LabelURL:       mockLabelData,
			Status:         "booked",
		}},
	}, nil
}

// TrackShipment returns a mock Hermes tracking response with one canned event.
func (m *MockHermesAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockHermesAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "0000",
			NormalizedStatus: StatusBooked,
			Location:         "Hamburg, DE",
			Details:          "The shipment has been notified to Hermes electronically.",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "hermes",
		Status:            "0000",
		NormalizedStatus:  StatusBooked,
		OriginalStatus:    "0000",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}

// FetchLabel returns a mock label response.
func (m *MockHermesAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "hermes",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for Hermes.
func (m *MockHermesAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Hermes", "shipment cancellation",
		"the HSI API does not support cancellation of individual shipment orders; contact Hermes customer service")
}

// UpdateShipment returns unsupported for Hermes.
func (m *MockHermesAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Hermes", "post-booking update", "")
}

// ── ManifestAdapter ───────────────────────────────────────────────────────────

// BookPickup returns a mock Hermes pickup order response.
func (m *MockHermesAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("hermes: pickup date is required")
	}
	return &PickupResponse{
		Carrier:            "hermes",
		ConfirmationNumber: fmt.Sprintf("%011d", rand.Intn(100000000000)), //nolint:gosec // mock data
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for Hermes.
func (m *MockHermesAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Hermes", "pickup update",
		"the HSI API has no update endpoint for pickup orders — cancel the existing pickup and create a new one")
}

// CancelPickup returns success for any confirmation number in mock mode.
func (m *MockHermesAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return nil
}

// CloseManifest is not supported for Hermes.
func (m *MockHermesAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Hermes", "manifest close", "no end-of-day manifest endpoint exists in the HSI API")
}

// GetPickupAvailability is not supported for Hermes.
func (m *MockHermesAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Hermes", "pickup availability",
		"no dedicated availability endpoint exists in the HSI API")
}

// ── PickupQuerier ─────────────────────────────────────────────────────────────

// GetPickupByID returns a mock pickup order for the given order ID.
func (m *MockHermesAdapter) GetPickupByID(_ context.Context, orderID string) (*PickupInfo, error) {
	return &PickupInfo{
		ID:                 orderID,
		Carrier:            "hermes",
		Status:             "CREATED",
		ConfirmationNumber: orderID,
		ReadyTime:          "BETWEEN_10_AND_13",
	}, nil
}

// ListPickups returns a mock paged list of pickup orders.
func (m *MockHermesAdapter) ListPickups(_ context.Context, req ListPickupsRequest) (*PickupList, error) {
	size := req.Size
	if size <= 0 {
		size = 20
	}
	return &PickupList{
		Carrier:    "hermes",
		Page:       req.Page,
		Count:      1,
		TotalPages: 1,
		PerPage:    size,
		Items: []PickupInfo{
			{
				ID:                 "01234567890",
				Carrier:            "hermes",
				Status:             "CREATED",
				ConfirmationNumber: "01234567890",
				ReadyTime:          "BETWEEN_10_AND_13",
			},
		},
	}, nil
}

// GetCutoffTime is not supported for Hermes.
func (m *MockHermesAdapter) GetCutoffTime(_ context.Context, _, _ string) (*PickupCutoffTime, error) {
	return nil, notSupported("Hermes", "pickup cutoff time", "no cutoff-time endpoint exists in the HSI API")
}

// NewMockHermesAdapter returns a new MockHermesAdapter with default behaviour.
func NewMockHermesAdapter() *MockHermesAdapter {
	return &MockHermesAdapter{}
}
