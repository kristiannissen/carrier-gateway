// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dhl_ecommerce_uk.go.
package adapter

import (
	"context"
)

// MockDHLEcomUKAdapter is a mock implementation of CarrierAdapter and ManifestAdapter
// for DHL eCommerce UK. It returns canned responses without making network calls.
type MockDHLEcomUKAdapter struct{}

// BookShipment returns a canned booking response.
func (m *MockDHLEcomUKAdapter) BookShipment(_ context.Context, req BookingRequest) (*BookingResponse, error) {
	colli := make([]ColliResponse, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		colli[i] = ColliResponse{
			ID:             c.ID,
			Reference:      c.Reference,
			TrackingNumber: "DHLUK" + c.ID,
			LabelURL:       mockLabelData,
			Status:         "booked",
		}
	}
	masterID := ""
	if len(colli) > 0 {
		masterID = colli[0].TrackingNumber
	}
	return &BookingResponse{
		TrackingNumber: masterID,
		ShipmentID:     masterID,
		Carrier:        "dhl_ecommerce_uk",
		Status:         "booked",
		Colli:          colli,
		BetaWarning:    "DHL eCommerce UK integration is in beta",
	}, nil
}

// TrackShipment returns a canned in-transit tracking response.
func (m *MockDHLEcomUKAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "dhl_ecommerce_uk",
		Status:           "transit",
		NormalizedStatus: StatusInTransit,
		OriginalStatus:   "transit",
		Events: []TrackingEvent{
			{
				Timestamp:        "2024-01-01T10:00:00Z",
				Status:           "transit",
				NormalizedStatus: StatusInTransit,
				Location:         "London Hub",
				Details:          "Shipment in transit",
			},
		},
	}, nil
}

// FetchLabel returns a canned label response.
func (m *MockDHLEcomUKAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	_, ok := dhlUKLabelFormat(req.Format)
	if !ok {
		return nil, unsupportedFormat("DHL eCommerce UK", req.Format, LabelFormatZPL, LabelFormatPDF, LabelFormatPNG)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dhl_ecommerce_uk",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns a canned cancellation response.
func (m *MockDHLEcomUKAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dhl_ecommerce_uk",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported for DHL eCommerce UK.
func (m *MockDHLEcomUKAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "update shipment", "use /shipping/v1/amendment via DHL portal or contact DHL customer service")
}

// BookPickup returns a canned pickup response.
func (m *MockDHLEcomUKAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	return &PickupResponse{
		Carrier:            "dhl_ecommerce_uk",
		ConfirmationNumber: "A000000001",
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for DHL eCommerce UK.
func (m *MockDHLEcomUKAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "update pickup", "cancel the existing pickup and book a new one")
}

// CancelPickup is not supported for DHL eCommerce UK.
func (m *MockDHLEcomUKAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DHL eCommerce UK", "cancel pickup", "contact DHL customer service")
}

// CloseManifest is not supported for DHL eCommerce UK.
func (m *MockDHLEcomUKAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "close manifest", "DHL eCommerce UK has no manifest API — shipments are automatically processed")
}

// GetPickupAvailability is not supported for DHL eCommerce UK.
func (m *MockDHLEcomUKAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "pickup availability", "proceed to BookPickup directly")
}
