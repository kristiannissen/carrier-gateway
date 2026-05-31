// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_bring.go.
package adapter

// MockBringAdapter is a mock implementation of the CarrierAdapter interface for Bring.
type MockBringAdapter struct{}

// BookShipment mocks booking a shipment with Bring.
func (a *MockBringAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	return &BookingResponse{
		TrackingNumber: "BR123456789NO",
		LabelURL:       "https://example.com/mock-bring-label.png",
		Carrier:        "bring",
	}, nil
}

// TrackShipment mocks tracking a shipment with Bring.
func (a *MockBringAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Shipment Accepted",
				Location:  "Oslo",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving service points for Bring.
func (a *MockBringAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "BR001",
			Name: "Bring Service Point 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "0123",
				City:       "Oslo",
				Country:    "NO",
			},
		},
	}, nil
}
