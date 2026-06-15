// Package adapter provides a mock Speedy CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_speedy.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockSpeedyAdapter implements CarrierAdapter, ManifestAdapter, PickupQuerier,
// ReturnAdapter, and ReturnQuerier with pre-canned Speedy responses.
//
// Each method can be overridden via its corresponding Func field:
//
//	a := &MockSpeedyAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockSpeedyAdapter struct {
	BookShipmentFunc   func(BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc  func(string) (*TrackingResponse, error)
	FetchLabelFunc     func(LabelRequest) (*LabelResponse, error)
	CancelShipmentFunc func(string) (*CancelResponse, error)
}

// BookShipment returns a mock Speedy booking response.
func (m *MockSpeedyAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("speedy: shipment must contain at least one colli")
	}

	zap.L().Info("MockSpeedyAdapter: returning mock booking response")

	shipmentID := fmt.Sprintf("SPD%010d", rand.Intn(1_000_000_000)) //nolint:gosec // mock data
	colli := make([]ColliResponse, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcelID := fmt.Sprintf("SPD%010d-%d", rand.Intn(1_000_000_000), i+1) //nolint:gosec // mock data
		colli[i] = ColliResponse{
			ID:             c.ID,
			TrackingNumber: parcelID,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		TrackingNumber: colli[0].TrackingNumber,
		ShipmentID:     shipmentID,
		Carrier:        "speedy",
		Status:         "booked",
		Colli:          colli,
	}, nil
}

// TrackShipment returns a mock Speedy tracking response with one canned event.
func (m *MockSpeedyAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockSpeedyAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "11",
			NormalizedStatus: StatusPickedUp,
			Location:         "Sofia, BG",
			Details:          "Shipment accepted at depot",
		},
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "1",
			NormalizedStatus: StatusBooked,
			Location:         "Sofia, BG",
			Details:          "Shipment created",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "speedy",
		Status:            "11",
		NormalizedStatus:  StatusPickedUp,
		OriginalStatus:    "11",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}

// FetchLabel returns a mock label response.
func (m *MockSpeedyAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if m.FetchLabelFunc != nil {
		return m.FetchLabelFunc(req)
	}
	switch req.Format {
	case LabelFormatPDF, LabelFormatZPL, LabelFormatZPLGK, "":
	default:
		return nil, unsupportedFormat("Speedy", req.Format, LabelFormatPDF, LabelFormatZPL)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "speedy",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a mock cancellation response.
func (m *MockSpeedyAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if m.CancelShipmentFunc != nil {
		return m.CancelShipmentFunc(trackingNumber)
	}
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "speedy",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Speedy", "post-booking update", "")
}

// BookPickup returns a mock pickup response.
func (m *MockSpeedyAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	orderID := fmt.Sprintf("SPDPU%08d", rand.Intn(100_000_000)) //nolint:gosec // mock data
	date := req.Pickup.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	return &PickupResponse{
		Carrier:            "speedy",
		ConfirmationNumber: orderID,
		Date:               date,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Speedy", "pickup update", "")
}

// CancelPickup returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("Speedy", "pickup cancellation", "")
}

// CloseManifest returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Speedy", "close manifest", "")
}

// GetPickupAvailability returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Speedy", "pickup availability", "")
}

// GetCutoffTime returns a mock cutoff time.
func (m *MockSpeedyAdapter) GetCutoffTime(_ context.Context, postalCode, _ string) (*PickupCutoffTime, error) {
	return &PickupCutoffTime{
		Carrier:    "speedy",
		PostalCode: postalCode,
		CutoffTime: "13:00",
	}, nil
}

// GetPickupByID returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) GetPickupByID(_ context.Context, _ string) (*PickupInfo, error) {
	return nil, notSupported("Speedy", "get pickup by ID", "")
}

// ListPickups returns ErrNotSupported for Speedy.
func (m *MockSpeedyAdapter) ListPickups(_ context.Context, _ ListPickupsRequest) (*PickupList, error) {
	return nil, notSupported("Speedy", "list pickups", "")
}

// BookReturn returns a mock return booking response.
func (m *MockSpeedyAdapter) BookReturn(_ context.Context, req ReturnRequest) (*ReturnResponse, error) {
	trackID := fmt.Sprintf("SPDRET%08d", rand.Intn(100_000_000)) //nolint:gosec // mock data
	shipID := fmt.Sprintf("SPD%010d", rand.Intn(1_000_000_000))  //nolint:gosec // mock data
	_ = req
	return &ReturnResponse{
		ShipmentID:     shipID,
		TrackingNumber: trackID,
		Carrier:        "speedy",
		Status:         "booked",
	}, nil
}

// FetchReturnLabel returns a mock return label.
func (m *MockSpeedyAdapter) FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	return m.FetchLabel(ctx, req)
}

// GetReturnShipment returns a mock secondary shipment info.
func (m *MockSpeedyAdapter) GetReturnShipment(_ context.Context, shipmentID string) (*ReturnShipmentInfo, error) {
	trackID := fmt.Sprintf("SPDRET%08d", rand.Intn(100_000_000)) //nolint:gosec // mock data
	return &ReturnShipmentInfo{
		ID:      shipmentID,
		Carrier: "speedy",
		Parcels: []ReturnParcelInfo{
			{ID: trackID, TrackingNumber: trackID},
		},
	}, nil
}

// NewMockSpeedyAdapter returns a MockSpeedyAdapter with default behaviour.
func NewMockSpeedyAdapter() *MockSpeedyAdapter {
	return &MockSpeedyAdapter{}
}
