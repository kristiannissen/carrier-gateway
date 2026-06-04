// Package adapter provides a mock Bring CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_bring.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// MockBringAdapter implements CarrierAdapter with pre-canned Bring responses.
// All three methods can be overridden via their corresponding Func fields:
//
//	adapter := &MockBringAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockBringAdapter struct {
	BookShipmentFunc     func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc    func(trackingNumber string) (*TrackingResponse, error)
	GetServicePointsFunc func(location Location) ([]ServicePoint, error)
}

// BookShipment returns a mock Bring booking response, applying the same
// validation as the real BringAdapter so tests catch input errors without a live API.
func (m *MockBringAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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

	zap.L().Info("MockBringAdapter: returning mock booking response")

	consignment := fmt.Sprintf("BR%09dNO", rand.Intn(1000000000))

	return &BookingResponse{
		TrackingNumber: consignment,
		LabelURL:       fmt.Sprintf("https://mock.bring.com/labels/%s.pdf", consignment),
		Carrier:        "bring",
		Cost:           125.50,
		Currency:       "NOK",
		ServiceLevel:   "Standard",
		Status:         "booked",
	}, nil
}

// TrackShipment returns a mock Bring tracking response with three canned events.
func (m *MockBringAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	zap.L().Info("MockBringAdapter: returning mock tracking response")

	events := []TrackingEvent{
		{
			Timestamp: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
			Status:    "Picked Up",
			Location:  "Oslo, NO",
		},
		{
			Timestamp: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			Status:    "In Transit",
			Location:  "Gothenburg, SE",
		},
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Status:    "Delivered",
			Location:  "Stockholm, SE",
		},
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "bring",
		Status:            "Delivered",
		EstimatedDelivery: time.Now().UTC().Format("2006-01-02"),
		Events:            events,
	}, nil
}

// GetServicePoints returns mock Bring pickup point locations.
func (m *MockBringAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	if m.GetServicePointsFunc != nil {
		return m.GetServicePointsFunc(location)
	}

	zap.L().Info("MockBringAdapter: returning mock service points")

	return []ServicePoint{
		{
			ID:   "BR001",
			Name: "Bring Service Point Oslo",
			Address: Address{
				Name:       "Bring Service Point Oslo",
				Street:     "Mock Street 1",
				PostalCode: "0123",
				City:       "Oslo",
				Country:    "NO",
			},
			Services: []string{"Pickup", "Dropoff"},
		},
	}, nil
}

// NewMockBringAdapter returns a new MockBringAdapter with default behaviour.
func NewMockBringAdapter() *MockBringAdapter {
	return &MockBringAdapter{}
}
