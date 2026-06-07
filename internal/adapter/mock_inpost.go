// Package adapter provides a mock InPost CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_inpost.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockInPostAdapter implements CarrierAdapter with pre-canned InPost responses.
// All three methods can be overridden via their corresponding Func fields:
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

	shipmentID := fmt.Sprintf("INPOST-%x", rand.Uint32())             //nolint:gosec // mock data, not security-sensitive
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
			Timestamp: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:    "Picked Up",
			Location:  "Warsaw, PL",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Status:    "In Transit",
			Location:  "Krakow, PL",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "inpost",
		Status:            "In Transit",
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

// NewMockInPostAdapter returns a new MockInPostAdapter with default behaviour.
func NewMockInPostAdapter() *MockInPostAdapter {
	return &MockInPostAdapter{}
}
