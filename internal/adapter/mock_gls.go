// Package adapter provides a mock GLS CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_gls.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"go.uber.org/zap"
)

// MockGLSAdapter implements CarrierAdapter with pre-canned GLS responses.
// All three methods can be overridden via their corresponding Func fields:
//
//	adapter := &MockGLSAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockGLSAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock GLS booking response, applying the same
// validation as the real GLSAdapter so tests catch input errors without a live API.
func (m *MockGLSAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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

	zap.L().Info("MockGLSAdapter: returning mock booking response")

	// Return shipments use the Shop Returns Customer Plus API v3.
	// Mock a returnOrderId as ShipmentID and a trackId as TrackingNumber.
	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		trackID := fmt.Sprintf("GLS-RET-%09d", rand.Intn(1000000000)) //nolint:gosec // mock data
		orderID := fmt.Sprintf("RO-%09d", rand.Intn(1000000000))       //nolint:gosec // mock data
		colli := make([]ColliResponse, len(request.Shipment.Colli))
		for i, c := range request.Shipment.Colli {
			colli[i] = ColliResponse{ID: c.ID, TrackingNumber: trackID, Status: "booked"}
		}
		return &BookingResponse{
			TrackingNumber: trackID,
			ShipmentID:     orderID,
			Carrier:        "gls",
			Status:         "booked",
			Colli:          colli,
		}, nil
	}

	colliResponses := make([]ColliResponse, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		trackID := fmt.Sprintf("GLS%09dDK-%d", rand.Intn(1000000000), i+1) //nolint:gosec // mock data, not security-sensitive
		colliResponses[i] = ColliResponse{
			ID:             c.ID,
			TrackingNumber: trackID,
			Status:         "booked",
		}
	}

	parent := fmt.Sprintf("GLS%09dDK", rand.Intn(1000000000)) //nolint:gosec // mock data, not security-sensitive

	return &BookingResponse{
		TrackingNumber: parent,
		LabelURL:       fmt.Sprintf("https://mock.gls-group.eu/labels/%s.pdf", parent),
		Carrier:        "gls",
		Status:         "booked",
		Colli:          colliResponses,
	}, nil
}

// TrackShipment returns a mock GLS tracking response with one canned event.
func (m *MockGLSAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockGLSAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "Shipment Accepted",
			NormalizedStatus: StatusUnknown, // GLS StatusCode enum not yet available
			Location:         "Copenhagen, DK",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "gls",
		Status:            "Shipment Accepted",
		NormalizedStatus:  StatusUnknown, // GLS StatusCode enum not yet available
		OriginalStatus:    "Shipment Accepted",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}


// FetchLabel returns a mock label response.
func (m *MockGLSAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "gls",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a mock cancellation response.
func (m *MockGLSAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment returns unsupported for GLS.
func (m *MockGLSAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("GLS", "post-booking update", "")
}

// NewMockGLSAdapter returns a new MockGLSAdapter with default behaviour.
func NewMockGLSAdapter() *MockGLSAdapter {
	return &MockGLSAdapter{}
}
