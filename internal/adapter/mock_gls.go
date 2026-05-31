// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_gls.go.
package adapter

// MockGLSAdapter is a mock implementation of the CarrierAdapter interface for GLS.
type MockGLSAdapter struct{}

// BookShipment mocks booking a shipment with GLS.
func (a *MockGLSAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	return &BookingResponse{
		TrackingNumber: "GLS123456789DK",
		LabelURL:       "https://example.com/mock-gls-label.png",
		Carrier:        "gls",
	}, nil
}

// TrackShipment mocks tracking a shipment with GLS.
func (a *MockGLSAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Shipment Accepted",
				Location:  "Copenhagen",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving service points for GLS.
func (a *MockGLSAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "GLS001",
			Name: "GLS ParcelShop 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "1234",
				City:       "Copenhagen",
				Country:    "DK",
			},
		},
	}, nil
}
