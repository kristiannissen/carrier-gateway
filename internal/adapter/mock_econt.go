// Package adapter provides the Econt mock adapter for testing.
// This file is located at /internal/adapter/mock_econt.go.
package adapter

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"time"
)

// MockEcontAdapter satisfies CarrierAdapter, ManifestAdapter, and PickupQuerier
// with deterministic canned responses. Used when ECONT_USERNAME is not set or
// MOCK_MODE=true.
type MockEcontAdapter struct{}

// BookShipment returns a canned booking response.
func (m *MockEcontAdapter) BookShipment(_ context.Context, r BookingRequest) (*BookingResponse, error) {
	colliID := ""
	if len(r.Shipment.Colli) > 0 {
		colliID = r.Shipment.Colli[0].ID
	}
	num := "MOCK-ECONT-" + colliID
	return &BookingResponse{
		TrackingNumber: num,
		Carrier:        "econt",
		Status:         "booked",
		Colli:          []ColliResponse{{ID: colliID, TrackingNumber: num, Status: "booked"}},
	}, nil
}

// TrackShipment returns a canned tracking response.
func (m *MockEcontAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "econt",
		Status:           "Prepared in eEcont",
		NormalizedStatus: StatusBooked,
		OriginalStatus:   "Prepared in eEcont",
		Events: []TrackingEvent{{
			Timestamp:        "2026-01-01T10:00:00",
			Status:           "client",
			NormalizedStatus: StatusBooked,
			Details:          "Shipment prepared in eEcont",
		}},
	}, nil
}

// FetchLabel returns a minimal valid base64 PDF stub.
func (m *MockEcontAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != "" && req.Format != LabelFormatPDF {
		return nil, notSupported("Econt", "label format "+string(req.Format), "only PDF is supported")
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "econt",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 mock")),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment returns a canned cancellation response.
func (m *MockEcontAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "econt",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment returns a canned update response listing the non-empty fields.
func (m *MockEcontAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	updated := make([]string, 0, 4)
	if req.ReceiverPhone != "" {
		updated = append(updated, "phone")
	}
	if req.ReceiverEmail != "" {
		updated = append(updated, "email")
	}
	if req.Weight != 0 {
		updated = append(updated, "weight")
	}
	if req.ServicePointID != "" {
		updated = append(updated, "servicePointId")
	}
	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "econt",
		Status:         "updated",
		UpdatedFields:  updated,
	}, nil
}

// BookPickup returns a mock pickup response.
func (m *MockEcontAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	date := req.Pickup.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	readyTime := req.Pickup.ReadyTime
	if readyTime == "" {
		readyTime = "09:00"
	}
	closeTime := req.Pickup.CloseTime
	if closeTime == "" {
		closeTime = "18:00"
	}
	return &PickupResponse{
		Carrier:            "econt",
		ConfirmationNumber: fmt.Sprintf("MOCK-ECONT-PU-%08d", rand.Intn(100_000_000)), //nolint:gosec // mock data
		Date:               date,
		ReadyTime:          readyTime,
		CloseTime:          closeTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Econt", "pickup update", "")
}

// CancelPickup returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("Econt", "pickup cancellation", "")
}

// CloseManifest returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Econt", "close manifest", "")
}

// GetPickupAvailability returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Econt", "pickup availability", "")
}

// GetPickupByID returns a mock pickup status.
func (m *MockEcontAdapter) GetPickupByID(_ context.Context, orderID string) (*PickupInfo, error) {
	return &PickupInfo{ID: orderID, Carrier: "econt", Status: "CREATED"}, nil
}

// ListPickups returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) ListPickups(_ context.Context, _ ListPickupsRequest) (*PickupList, error) {
	return nil, notSupported("Econt", "list pickups", "")
}

// GetCutoffTime returns ErrNotSupported for Econt.
func (m *MockEcontAdapter) GetCutoffTime(_ context.Context, _, _ string) (*PickupCutoffTime, error) {
	return nil, notSupported("Econt", "pickup cutoff time", "")
}
