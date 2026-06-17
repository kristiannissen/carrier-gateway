// Package adapter provides a mock PostNL CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_postnl.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockPostNLAdapter implements CarrierAdapter and ReturnAdapter with pre-canned
// PostNL responses. All methods can be overridden via their corresponding Func fields:
//
//	adapter := &MockPostNLAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockPostNLAdapter struct {
	BookShipmentFunc    func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc   func(trackingNumber string) (*TrackingResponse, error)
	FetchLabelFunc      func(req LabelRequest) (*LabelResponse, error)
	CancelShipmentFunc  func(trackingNumber string) (*CancelResponse, error)
	UpdateShipmentFunc  func(req UpdateRequest) (*UpdateResponse, error)
	BookReturnFunc      func(req ReturnRequest) (*ReturnResponse, error)
	FetchReturnLabelFunc func(req LabelRequest) (*LabelResponse, error)
}

// postnlMockBarcode generates a random mock 3S-format PostNL barcode.
func postnlMockBarcode() string {
	return fmt.Sprintf("3SMOCK%09d", rand.Intn(1000000000)) //nolint:gosec // mock data, not security-sensitive
}

// BookShipment returns a mock PostNL booking response.
func (m *MockPostNLAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("postnl: TotalWeight is required and must be greater than 0")
	}

	zap.L().Info("MockPostNLAdapter: returning mock booking response")

	barcode := postnlMockBarcode()
	return &BookingResponse{
		TrackingNumber: barcode,
		Carrier:        "postnl",
		Status:         "booked",
		BetaWarning:    "PostNL integration is in beta",
	}, nil
}

// TrackShipment returns a mock PostNL tracking response with three canned events.
func (m *MockPostNLAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockPostNLAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "A01 Electronic pre-announcement",
			NormalizedStatus: StatusBooked,
			Location:         "NL",
		},
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "B01 Accepted at PostNL depot",
			NormalizedStatus: StatusPickedUp,
			Location:         "Amsterdam, NL",
		},
		{
			Timestamp:        time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "J05 Driver en route to recipient",
			NormalizedStatus: StatusOutForDelivery,
			Location:         "Amsterdam, NL",
		},
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "postnl",
		Status:           "Shipment out for delivery",
		NormalizedStatus: StatusOutForDelivery,
		Events:           events,
	}, nil
}

// FetchLabel returns a mock PostNL label response.
func (m *MockPostNLAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if m.FetchLabelFunc != nil {
		return m.FetchLabelFunc(req)
	}

	zap.L().Info("MockPostNLAdapter: returning mock label response")

	f := req.Format
	if f == "" {
		f = LabelFormatPDF
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnl",
		Format:         f,
		Data:           "bW9jay1sYWJlbC1kYXRh", // base64("mock-label-data")
		MimeType:       MimeTypeForFormat(f),
	}, nil
}

// CancelShipment always returns ErrNotSupported, matching the live adapter.
func (m *MockPostNLAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if m.CancelShipmentFunc != nil {
		return m.CancelShipmentFunc(trackingNumber)
	}
	return nil, notSupported("PostNL", "cancellation",
		"contact PostNL customer support or use the Business Centre portal")
}

// UpdateShipment always returns ErrNotSupported, matching the live adapter.
func (m *MockPostNLAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if m.UpdateShipmentFunc != nil {
		return m.UpdateShipmentFunc(req)
	}
	return nil, notSupported("PostNL", "shipment update",
		"post-booking updates are not available via the PostNL PNP v4 API")
}

// BookReturn returns a mock PostNL return booking response.
func (m *MockPostNLAdapter) BookReturn(ctx context.Context, req ReturnRequest) (*ReturnResponse, error) {
	if m.BookReturnFunc != nil {
		return m.BookReturnFunc(req)
	}

	zap.L().Info("MockPostNLAdapter: returning mock return booking response")

	barcode := postnlMockBarcode()
	return &ReturnResponse{
		TrackingNumber: barcode,
		Carrier:        "postnl",
		Status:         "booked",
	}, nil
}

// FetchReturnLabel returns a mock PostNL return label response.
func (m *MockPostNLAdapter) FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if m.FetchReturnLabelFunc != nil {
		return m.FetchReturnLabelFunc(req)
	}
	return m.FetchLabel(ctx, req)
}
