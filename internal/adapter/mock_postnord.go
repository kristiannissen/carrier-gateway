// Package adapter provides a mock implementation of the PostNord CarrierAdapter for testing and demo purposes.
// This file is located at /internal/adapter/mock_postnord.go.
package adapter

import (
	"fmt"
	"log/slog"
	"math/rand"
	"time"
)

// MockPostNordAdapter implements CarrierAdapter for testing and demo purposes.
// It returns predefined mock responses for PostNord API calls.
type MockPostNordAdapter struct {
	// Fields for customizing mock responses (optional)
	BookShipmentFunc    func(request BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc   func(trackingNumber string) (*TrackingResponse, error)
	GetServicePointsFunc func(location Location) ([]ServicePoint, error)
}

// BookShipment returns a mock booking response.
func (m *MockPostNordAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(request)
	}

	// Log mock mode warning
	slog.Info("MockPostNordAdapter: Returning mock booking response")

	// Generate mock tracking numbers for each colli
	colliResponses := make([]ColliResponse, len(request.Shipment.Colli))
	for i, colli := range request.Shipment.Colli {
		colliResponses[i] = ColliResponse{
			ID:             colli.ID,
			Reference:      colli.Reference,
			TrackingNumber: fmt.Sprintf("PN%09dDK-%d", rand.Intn(1000000000), i+1),
			LabelURL:       fmt.Sprintf("https://mock.postnord.com/labels/%s.pdf", colli.ID),
			Status:         "booked",
		}
	}

	// Generate parent tracking number
	parentTrackingNumber := fmt.Sprintf("PN%09dDK", rand.Intn(1000000000))

	return &BookingResponse{
		ShipmentID:     fmt.Sprintf("shipment_%d", rand.Intn(1000000)),
		TrackingNumber: parentTrackingNumber,
		LabelURL:       fmt.Sprintf("https://mock.postnord.com/labels/%s.pdf", parentTrackingNumber),
		Carrier:        "postnord",
		Cost:           125.50,
		Currency:       "DKK",
		ServiceLevel:   "Standard",
		Status:         "booked",
		Colli:          colliResponses,
	}, nil
}

// TrackShipment returns a mock tracking response.
func (m *MockPostNordAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	// Log mock mode warning
	slog.Info("MockPostNordAdapter: Returning mock tracking response")

	// Generate mock events
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

	// Generate mock colli tracking
	colliTracking := []ColliTracking{
		{
			ID:             "colli_1",
			Reference:      "BOX-001",
			TrackingNumber: trackingNumber + "-1",
			Status:         "In Transit",
			Events:         events,
		},
	}

	return &TrackingResponse{
		ShipmentID:       fmt.Sprintf("shipment_%d", rand.Intn(1000000)),
		TrackingNumber:   trackingNumber,
		Carrier:          "postnord",
		Status:           "In Transit",
		EstimatedDelivery: time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02"),
		Events:           events,
		Colli:            colliTracking,
	}, nil
}

// GetServicePoints returns mock service points.
func (m *MockPostNordAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	if m.GetServicePointsFunc != nil {
		return m.GetServicePointsFunc(location)
	}

	// Log mock mode warning
	slog.Info("MockPostNordAdapter: Returning mock service points")

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
			Services:    []string{"Pickup", "Dropoff"},
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
			Services:    []string{"Pickup"},
		},
	}, nil
}

// NewMockPostNordAdapter creates a new mock PostNord adapter.
func NewMockPostNordAdapter() *MockPostNordAdapter {
	return &MockPostNordAdapter{}
}
