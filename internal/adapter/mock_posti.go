// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_posti.go.
package adapter

// MockPostiAdapter is a mock implementation of the CarrierAdapter interface for Posti.
type MockPostiAdapter struct{}

// BookShipment mocks booking a shipment with Posti.
func (a *MockPostiAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	return &BookingResponse{
		TrackingNumber: "POSTI123456789FI",
		LabelURL:       "https://example.com/mock-posti-label.png",
		Carrier:        "posti",
	}, nil
}

// TrackShipment mocks tracking a shipment with Posti.
func (a *MockPostiAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Shipment Accepted",
				Location:  "Helsinki",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving service points for Posti.
func (a *MockPostiAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "POSTI001",
			Name: "Posti Pickup Point 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "00100",
				City:       "Helsinki",
				Country:    "FI",
			},
		},
	}, nil
}
