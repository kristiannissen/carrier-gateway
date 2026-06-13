// Package adapter provides a mock DPD CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_dpd.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// MockDPDAdapter implements CarrierAdapter and ManifestAdapter with pre-canned
// DPD responses. Override individual methods via the corresponding Func fields:
//
//	a := &MockDPDAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockDPDAdapter struct {
	BookShipmentFunc  func(req BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock DPD booking response.
// TrackingNumber is a synthetic 14-digit parcel number; ShipmentID is a UUID-style string.
func (m *MockDPDAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(req)
	}
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	shipmentID := fmt.Sprintf("mock-dpd-%08d", rand.Intn(100000000)) //nolint:gosec // mock data
	colli := make([]ColliResponse, len(req.Shipment.Colli))
	firstParcel := ""
	for i, c := range req.Shipment.Colli {
		pn := fmt.Sprintf("%014d", rand.Intn(100000000)) //nolint:gosec // mock data
		if i == 0 {
			firstParcel = pn
		}
		colli[i] = ColliResponse{ID: c.ID, TrackingNumber: pn, Status: "booked"}
	}

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: firstParcel,
		Carrier:        "dpd",
		Status:         "booked",
		Colli:          colli,
		BetaWarning:    "DPD adapter is in beta — validate in sandbox before production use",
	}, nil
}

// TrackShipment returns a mock DPD tracking response with one canned event.
func (m *MockDPDAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "02",
			NormalizedStatus: StatusPickedUp,
			Location:         "Vilnius, LT",
			Details:          "Parcel accepted in terminal",
		},
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "dpd",
		Status:           "02",
		NormalizedStatus: StatusPickedUp,
		OriginalStatus:   "02",
		Events:           events,
	}, nil
}

// FetchLabel returns a mock label response.
func (m *MockDPDAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a mock cancellation response.
func (m *MockDPDAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dpd",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported by DPD.
func (m *MockDPDAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD", "post-booking update", "cancel and rebook")
}

// BookPickup returns a mock pickup confirmation.
func (m *MockDPDAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	return &PickupResponse{
		Carrier:            "dpd",
		ConfirmationNumber: req.Pickup.Date + "T" + req.Pickup.ReadyTime,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by DPD.
func (m *MockDPDAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD", "pickup update", "cancel and rebook")
}

// CancelPickup is not supported by the DPD Baltic API.
func (m *MockDPDAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DPD", "pickup cancellation", "not available in DPD Baltic API v1")
}

// CloseManifest is not supported by DPD.
func (m *MockDPDAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DPD", "manifest close", "pickup creation serves as the handover instruction")
}

// GetPickupAvailability is not supported by DPD.
func (m *MockDPDAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DPD", "pickup availability", "")
}

// NewMockDPDAdapter returns a MockDPDAdapter with default behaviour.
func NewMockDPDAdapter() *MockDPDAdapter {
	return &MockDPDAdapter{}
}
