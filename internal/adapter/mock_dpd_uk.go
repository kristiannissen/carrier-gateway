// Package adapter provides a mock DPD UK CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_dpd_uk.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
)

// MockDPDUKAdapter implements CarrierAdapter and ManifestAdapter with pre-canned
// DPD UK responses. Override individual methods via the corresponding Func fields:
//
//	a := &MockDPDUKAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockDPDUKAdapter struct {
	BookShipmentFunc  func(req BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock DPD UK booking response.
// TrackingNumber is a synthetic 14-digit parcel number; ShipmentID is a numeric string.
func (m *MockDPDUKAdapter) BookShipment(_ context.Context, req BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(req)
	}
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	shipmentID := fmt.Sprintf("%d", rand.Intn(900000000)+100000000) //nolint:gosec // mock data
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
		Carrier:        "dpd_uk",
		Status:         "booked",
		Colli:          colli,
		BetaWarning:    "DPD UK adapter is in beta — validate in sandbox before production use",
	}, nil
}

// TrackShipment returns a not-supported error — tracking is not yet implemented for DPD UK.
func (m *MockDPDUKAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("DPD UK", "shipment tracking",
		"tracking endpoint not yet confirmed — use https://track.dpd.co.uk")
}

// FetchLabel returns a mock label response.
func (m *MockDPDUKAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("DPD UK", req.Format, LabelFormatPDF)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd_uk",
		Format:         LabelFormatPDF,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment returns a not-supported error — cancellation is not yet implemented for DPD UK.
func (m *MockDPDUKAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DPD UK", "shipment cancellation",
		"cancellation endpoint not yet confirmed — cancel via the DPD UK Shipping portal")
}

// UpdateShipment is not supported for DPD UK.
func (m *MockDPDUKAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD UK", "post-booking update", "cancel and rebook")
}

// BookPickup is not supported for DPD UK.
func (m *MockDPDUKAdapter) BookPickup(_ context.Context, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD UK", "pickup booking",
		"set the collection date via collectionDate at booking time")
}

// UpdatePickup is not supported for DPD UK.
func (m *MockDPDUKAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD UK", "pickup update", "")
}

// CancelPickup is not supported for DPD UK.
func (m *MockDPDUKAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DPD UK", "pickup cancellation", "")
}

// CloseManifest is not supported for DPD UK.
func (m *MockDPDUKAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DPD UK", "manifest close", "")
}

// GetPickupAvailability is not supported for DPD UK.
func (m *MockDPDUKAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DPD UK", "pickup availability", "")
}
