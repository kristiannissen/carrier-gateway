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
	BookShipmentFunc     func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc    func(trackingNumber string) (*TrackingResponse, error)
	GetServicePointsFunc func(location Location) ([]ServicePoint, error)
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
			TrackingNumber: fmt.Sprintf("PN%09dDK-%d", rand.Intn(1000000000), i+1),
			LabelURL:       fmt.Sprintf("https://mock.postnord.com/labels/%d_%dg.pdf", i+1, weightGrams),
			Status:         "booked",
		}
	}

	parent := fmt.Sprintf("PN%09dDK", rand.Intn(1000000000))

	return &BookingResponse{
		ShipmentID:     fmt.Sprintf("shipment_%d", rand.Intn(1000000)),
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
			Timestamp: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:    "Picked Up",
			Location:  "Copenhagen, DK",
			Details:   "Package picked up at sender location",
		},
		{
			Timestamp: time.Now().Add(-12 * time.Hour).UTC().Format(time.RFC3339),
			Status:    "In Transit",
			Location:  "Malmö, SE",
			Details:   "Package arrived at Malmö hub",
		},
	}

	return &TrackingResponse{
		ShipmentID:        fmt.Sprintf("shipment_%d", rand.Intn(1000000)),
		TrackingNumber:    trackingNumber,
		Carrier:           "postnord",
		Status:            "In Transit",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:            events,
		Colli: []ColliTracking{
			{
				ID:             "1",
				TrackingNumber: trackingNumber + "-1",
				Status:         "In Transit",
				Events:         events,
			},
		},
	}, nil
}

// GetServicePoints returns two mock PostNord service points.
func (m *MockPostNordAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	if m.GetServicePointsFunc != nil {
		return m.GetServicePointsFunc(location)
	}

	zap.L().Info("MockPostNordAdapter: returning mock service points")

	return []ServicePoint{
		{
			ID:   "sp_123",
			Name: "PostNord Copenhagen",
			Address: Address{
				Name:       "PostNord Copenhagen",
				Street:     "Main Street 1",
				City:       "Copenhagen",
				PostalCode: "12345",
				Country:    "DK",
			},
			OpeningHours: "09:00-17:00",
			Services:     []string{"Pickup", "Dropoff"},
		},
		{
			ID:   "sp_456",
			Name: "PostNord Aarhus",
			Address: Address{
				Name:       "PostNord Aarhus",
				Street:     "Second Street 2",
				City:       "Aarhus",
				PostalCode: "8000",
				Country:    "DK",
			},
			OpeningHours: "08:00-16:00",
			Services:     []string{"Pickup"},
		},
	}, nil
}

// NewMockPostNordAdapter returns a new MockPostNordAdapter with default behaviour.
func NewMockPostNordAdapter() *MockPostNordAdapter {
	return &MockPostNordAdapter{}
}
