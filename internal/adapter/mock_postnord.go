// Package adapter provides a mock PostNord CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_postnord.go.
package adapter

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockPostNordAdapter implements CarrierAdapter with pre-canned responses.
// All three methods can be overridden via their corresponding Func fields,
// making it easy to inject specific responses or errors in tests:
//
//	adapter := &MockPostNordAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockPostNordAdapter struct {
	BookShipmentFunc  func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock booking response, applying the same validation
// as the real PostNordAdapter so tests catch input errors without a live API.
func (m *MockPostNordAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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

	zap.L().Info("MockPostNordAdapter: returning mock booking response")

	colliResponses := make([]ColliResponse, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		weightGrams := int(math.Round(c.Weight * 1000))
		colliResponses[i] = ColliResponse{
			ID:             fmt.Sprintf("%d", i+1),
			Reference:      c.ID,
			TrackingNumber: fmt.Sprintf("PN%09dDK-%d", rand.Intn(1000000000), i+1), //nolint:gosec // mock data, not security-sensitive
			LabelURL:       fmt.Sprintf("https://mock.postnord.com/labels/%d_%dg.pdf", i+1, weightGrams),
			Status:         "booked",
		}
	}

	parent := fmt.Sprintf("PN%09dDK", rand.Intn(1000000000)) //nolint:gosec // mock data, not security-sensitive

	return &BookingResponse{
		ShipmentID:     fmt.Sprintf("shipment_%d", rand.Intn(1000000)), //nolint:gosec // mock data, not security-sensitive
		TrackingNumber: parent,
		LabelURL:       fmt.Sprintf("https://mock.postnord.com/labels/%s.pdf", parent),
		Carrier:        "postnord",
		Cost:           125.50,
		Currency:       "DKK",
		ServiceLevel:   "1700",
		Status:         "booked",
		Colli:          colliResponses,
	}, nil
}

// TrackShipment returns a mock tracking response with two canned events.
func (m *MockPostNordAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockPostNordAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "INFORMED",
			NormalizedStatus: StatusBooked,
			Location:         "Copenhagen, DK",
			Details:          "Package picked up at sender location",
		},
		{
			Timestamp:        time.Now().Add(-12 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "IN_TRANSPORT",
			NormalizedStatus: StatusInTransit,
			Location:         "Malmö, SE",
			Details:          "Package arrived at Malmö hub",
		},
	}

	return &TrackingResponse{
		ShipmentID:        fmt.Sprintf("shipment_%d", rand.Intn(1000000)), //nolint:gosec // mock data, not security-sensitive
		TrackingNumber:    trackingNumber,
		Carrier:           "postnord",
		Status:            "IN_TRANSPORT",
		NormalizedStatus:  StatusInTransit,
		OriginalStatus:    "IN_TRANSPORT",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
		Colli: []ColliTracking{
			{
				ID:             "1",
				TrackingNumber: trackingNumber + "-1",
				Status:         "IN_TRANSPORT",
				Events:         events,
			},
		},
	}, nil
}

// FetchLabel returns a mock label response with minimal base64-encoded PDF data.
func (m *MockPostNordAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnord",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a mock cancel response.
func (m *MockPostNordAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	return &CancelResponse{TrackingNumber: trackingNumber, Carrier: "postnord", Status: "cancelled"}, nil
}

// UpdateShipment returns a mock update response.
func (m *MockPostNordAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	var fields []string
	if req.ReceiverPhone != "" {
		fields = append(fields, "phone")
	}
	if req.ReceiverEmail != "" {
		fields = append(fields, "email")
	}
	return &UpdateResponse{TrackingNumber: req.TrackingNumber, Carrier: "postnord", Status: "updated", UpdatedFields: fields}, nil
}

// NewMockPostNordAdapter returns a new MockPostNordAdapter with default behaviour.
func NewMockPostNordAdapter() *MockPostNordAdapter {
	return &MockPostNordAdapter{}
}
