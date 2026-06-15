// Package adapter provides a mock GLSNLAdapter for testing and local development.
// This file is located at /internal/adapter/mock_gls_nl.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// MockGLSNLAdapter implements CarrierAdapter and ManifestAdapter with pre-canned
// GLS NL responses. Override individual methods via the corresponding Func fields:
//
//	a := &MockGLSNLAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockGLSNLAdapter struct {
	BookShipmentFunc  func(req BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// NewMockGLSNLAdapter returns a MockGLSNLAdapter with default behaviour.
func NewMockGLSNLAdapter() *MockGLSNLAdapter {
	return &MockGLSNLAdapter{}
}

// BookShipment returns a mock GLS NL booking response with a synthetic unit number.
func (m *MockGLSNLAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(req)
	}
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	shipmentID := fmt.Sprintf("mock-gls-nl-%08d", rand.Intn(100000000)) //nolint:gosec // mock data
	colli := make([]ColliResponse, len(req.Shipment.Colli))
	firstUnit := ""
	for i, c := range req.Shipment.Colli {
		un := fmt.Sprintf("%014d", rand.Intn(100000000)) //nolint:gosec // mock data
		if i == 0 {
			firstUnit = un
		}
		colli[i] = ColliResponse{
			ID:             c.ID,
			TrackingNumber: un,
			LabelURL:       "data:application/pdf;base64," + mockLabelData,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: firstUnit,
		Carrier:        "gls_nl",
		Status:         "booked",
		Colli:          colli,
		BetaWarning:    "GLS NL regional adapter is in beta — validate in sandbox before production use",
	}, nil
}

// TrackShipment returns ErrNotSupported — the GLS NL API has no tracking endpoint.
func (m *MockGLSNLAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("GLS NL", "tracking",
		"use the GLS tracking portal or the unified GLS Group adapter (carrier \"gls\")")
}

// FetchLabel returns ErrNotSupported — the GLS NL API has no reprint endpoint.
func (m *MockGLSNLAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("GLS NL", "label reprint",
		"save the label from the booking response colli[].labelUrl")
}

// CancelShipment returns a mock cancellation response.
func (m *MockGLSNLAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls_nl",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported by the GLS NL API.
func (m *MockGLSNLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("GLS NL", "post-booking update", "cancel and rebook")
}

// BookPickup returns a mock pickup confirmation.
func (m *MockGLSNLAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	confirmation := req.Pickup.Date
	if confirmation == "" {
		confirmation = time.Now().Format("2006-01-02")
	}
	return &PickupResponse{
		Carrier:            "gls_nl",
		ConfirmationNumber: confirmation,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by the GLS NL API.
func (m *MockGLSNLAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("GLS NL", "pickup update", "cancel and rebook")
}

// CancelPickup returns nil (success) for mock purposes.
func (m *MockGLSNLAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return nil
}

// CloseManifest returns a mock manifest response confirming all tracking numbers.
func (m *MockGLSNLAdapter) CloseManifest(_ context.Context, req ManifestRequest) (*ManifestResponse, error) {
	return &ManifestResponse{
		Carrier:          "gls_nl",
		Date:             req.Date,
		Status:           "closed",
		ParcelsConfirmed: len(req.TrackingNumbers),
		Warnings:         []string{},
	}, nil
}

// GetPickupAvailability is not supported by the GLS NL API.
func (m *MockGLSNLAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("GLS NL", "pickup availability", "")
}
