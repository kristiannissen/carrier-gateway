// Package adapter provides a mock Evri CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_evri.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"

	"go.uber.org/zap"
)

// MockEvriAdapter implements CarrierAdapter with pre-canned Evri responses.
// BookShipmentFunc and FetchLabelFunc can be overridden to inject custom
// responses or errors in tests:
//
//	adapter := &MockEvriAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockEvriAdapter struct {
	BookShipmentFunc func(request BookingRequest) (*BookingResponse, error)
	FetchLabelFunc   func(req LabelRequest) (*LabelResponse, error)
}

// NewMockEvriAdapter returns a MockEvriAdapter with default behaviour.
func NewMockEvriAdapter() *MockEvriAdapter {
	return &MockEvriAdapter{}
}

// BookShipment returns a mock Evri booking response.
func (m *MockEvriAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("evri: shipment must contain at least one colli")
	}

	zap.L().Info("MockEvriAdapter: returning mock booking response")

	colliResponses := make([]ColliResponse, len(request.Shipment.Colli))
	primaryBarcode := ""
	for i, c := range request.Shipment.Colli {
		barcode := fmt.Sprintf("EV%018d", rand.Intn(1000000000000000000)) //nolint:gosec // mock data
		if primaryBarcode == "" {
			primaryBarcode = barcode
		}
		colliResponses[i] = ColliResponse{
			ID:             c.ID,
			TrackingNumber: barcode,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		TrackingNumber: primaryBarcode,
		Carrier:        "evri",
		Status:         "booked",
		Colli:          colliResponses,
		BetaWarning:    "Evri adapter is in beta — tracking, cancellation, and update are not yet supported",
	}, nil
}

// TrackShipment returns unsupported for Evri — no tracking endpoint exists in
// the Evri Classic API.
func (m *MockEvriAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("Evri", "shipment tracking",
		"the Evri Classic API does not expose a tracking endpoint; use the Evri consumer tracking website")
}

// FetchLabel returns a mock label response.
func (m *MockEvriAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if m.FetchLabelFunc != nil {
		return m.FetchLabelFunc(req)
	}

	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("evri: tracking number must not be empty")
	}

	if _, err := evriLabelAccept(req.Format); err != nil {
		return nil, err
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "evri",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for Evri.
func (m *MockEvriAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Evri", "shipment cancellation",
		"the Evri Classic API does not expose a cancellation endpoint")
}

// UpdateShipment returns unsupported for Evri.
func (m *MockEvriAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Evri", "post-booking update",
		"the Evri Classic API does not expose an update endpoint")
}
