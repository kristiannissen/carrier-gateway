// Package adapter provides a mock InPost CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_inpost.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"go.uber.org/zap"
)

// MockInPostAdapter implements CarrierAdapter, ManifestAdapter, and ReturnAdapter
// with pre-canned InPost responses suitable for testing and MOCK_MODE.
// All method behaviour can be overridden via the corresponding Func fields:
//
//	adapter := &MockInPostAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockInPostAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock InPost booking response, applying the same
// validation as the real InPostAdapter so tests catch input errors without a live API.
func (m *MockInPostAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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

	zap.L().Info("MockInPostAdapter: returning mock booking response")

	shipmentID := fmt.Sprintf("INPOST-%x", rand.Uint32())                //nolint:gosec // mock data, not security-sensitive
	trackingNumber := fmt.Sprintf("INPOST%09dPL", rand.Intn(1000000000)) //nolint:gosec // mock data, not security-sensitive

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		LabelURL:       fmt.Sprintf("https://mock.inpost.pl/labels/%s.pdf", shipmentID),
		Carrier:        "inpost",
		Cost:           8.00,
		Currency:       "PLN",
		Status:         "booked",
		LockerId:       "WAR001",
	}, nil
}

// TrackShipment returns a mock InPost tracking response with two canned events.
func (m *MockInPostAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockInPostAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "Picked Up",
			NormalizedStatus: StatusUnknown,
			Location:         "Warsaw, PL",
		},
		{
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			Status:           "In Transit",
			NormalizedStatus: StatusUnknown,
			Location:         "Krakow, PL",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "inpost",
		Status:            "In Transit",
		NormalizedStatus:  StatusUnknown,
		OriginalStatus:    "In Transit",
		EstimatedDelivery: time.Now().Add(24 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}

// FetchLabel returns a mock label response for InPost.
func (m *MockInPostAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "inpost",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for InPost.
func (m *MockInPostAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("InPost", "cancellation", "")
}

// UpdateShipment returns unsupported for InPost.
func (m *MockInPostAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("InPost", "post-booking update", "")
}

// ── ManifestAdapter ───────────────────────────────────────────────────────────

// BookPickup returns a mock pickup booking response.
// Enforces the same PL-only country gate as the real adapter.
func (m *MockInPostAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	if !strings.EqualFold(req.Address.Country, "PL") {
		return nil, notSupported("InPost", "pickup",
			fmt.Sprintf("pickups are only available in Poland (PL); got %q", req.Address.Country))
	}
	return &PickupResponse{
		Carrier:            "inpost",
		ConfirmationNumber: fmt.Sprintf("mock-pickup-%x", rand.Uint32()), //nolint:gosec // mock data
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for InPost.
func (m *MockInPostAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("InPost", "pickup update",
		"InPost has no pickup update endpoint — cancel the existing pickup and create a new one")
}

// CancelPickup returns success for any confirmation number in mock mode.
func (m *MockInPostAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return nil
}

// CloseManifest is not supported for InPost.
func (m *MockInPostAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("InPost", "manifest close",
		"InPost uses a drop-off locker network — no end-of-day manifest close is required")
}

// GetPickupAvailability is not supported for InPost.
func (m *MockInPostAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("InPost", "pickup availability",
		"use GET /pickups/v1/cutoff-time?postalCode=&countryCode= to check same-day pickup eligibility")
}

// ── ReturnAdapter ─────────────────────────────────────────────────────────────

// BookReturn returns a mock InPost return shipment response.
// Enforces the same PL/IT/GB country gate as the real adapter.
func (m *MockInPostAdapter) BookReturn(_ context.Context, req ReturnRequest) (*ReturnResponse, error) {
	country := strings.ToUpper(req.Sender.Country)
	if !inpostReturnCountries[country] {
		return nil, fmt.Errorf("inpost: returns are only available for PL, IT, and GB (got %q)", req.Sender.Country)
	}
	return &ReturnResponse{
		ShipmentID:     fmt.Sprintf("mock-return-%x", rand.Uint32()), //nolint:gosec // mock data
		TrackingNumber: "63031234567891234567890",
		DropOffCode:    "012345",
		Carrier:        "inpost",
		Status:         "booked",
	}, nil
}

// FetchReturnLabel returns a mock return label response.
func (m *MockInPostAdapter) FetchReturnLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "inpost",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// ── PickupQuerier ─────────────────────────────────────────────────────────────

// GetPickupByID returns a mock pickup order for the given order ID.
func (m *MockInPostAdapter) GetPickupByID(_ context.Context, orderID string) (*PickupInfo, error) {
	return &PickupInfo{
		ID:                 orderID,
		Carrier:            "inpost",
		Status:             "CREATED",
		ConfirmationNumber: "853828",
		ReadyTime:          "2000-10-31T09:00:00Z",
		CloseTime:          "2000-10-31T18:00:00Z",
	}, nil
}

// ListPickups returns a mock paged list of pickup orders.
func (m *MockInPostAdapter) ListPickups(_ context.Context, req ListPickupsRequest) (*PickupList, error) {
	size := req.Size
	if size <= 0 {
		size = 20
	}
	return &PickupList{
		Carrier:    "inpost",
		Page:       req.Page,
		Count:      1,
		TotalPages: 1,
		PerPage:    size,
		Items: []PickupInfo{
			{
				ID:                 "mock-pickup-id",
				Carrier:            "inpost",
				Status:             "CREATED",
				ConfirmationNumber: "853828",
			},
		},
	}, nil
}

// GetCutoffTime returns a mock cutoff time for the given postal code.
func (m *MockInPostAdapter) GetCutoffTime(_ context.Context, postalCode, _ string) (*PickupCutoffTime, error) {
	return &PickupCutoffTime{
		Carrier:    "inpost",
		PostalCode: postalCode,
		CutoffTime: "13:00:00",
	}, nil
}

// ── ReturnQuerier ─────────────────────────────────────────────────────────────

// GetReturnShipment returns a mock return shipment for the given shipment ID.
func (m *MockInPostAdapter) GetReturnShipment(_ context.Context, shipmentID string) (*ReturnShipmentInfo, error) {
	return &ReturnShipmentInfo{
		ID:             shipmentID,
		Carrier:        "inpost",
		ExpirationDate: "2025-12-01T12:00:00Z",
		Parcels: []ReturnParcelInfo{
			{
				ID:             "mock-parcel-id",
				TrackingNumber: "63031234567891234567890",
				DropOffCode:    "012345",
			},
		},
	}, nil
}

// NewMockInPostAdapter returns a new MockInPostAdapter with default behaviour.
func NewMockInPostAdapter() *MockInPostAdapter {
	return &MockInPostAdapter{}
}
