// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dao.go.
package adapter

// MockDAOAdapter is a mock implementation of the CarrierAdapter interface for DAO.
type MockDAOAdapter struct{}

// BookShipment mocks booking a shipment with DAO.
func (a *MockDAOAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	return &BookingResponse{
		TrackingNumber: "DAO123456789DK",
		LabelURL:       "https://example.com/mock-dao-label.png",
		Carrier:        "dao",
	}, nil
}

// TrackShipment mocks tracking a shipment with DAO.
func (a *MockDAOAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Status:         "In Transit",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "Pakke modtaget på fordelingscenter",
				Location:  "DAO Erritsø",
			},
		},
	}, nil
}

// GetServicePoints mocks retrieving parcel shops for DAO.
func (a *MockDAOAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	return []ServicePoint{
		{
			ID:   "DAO001",
			Name: "DAO Pakkeshop 1",
			Address: Address{
				Street:     "Mock Street 1",
				PostalCode: "1234",
				City:       "Copenhagen",
				Country:    "DK",
			},
		},
	}, nil
}
