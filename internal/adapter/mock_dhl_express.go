// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dhl_express.go.
package adapter

import (
	"context"
	"fmt"
)

// MockDHLExpressAdapter is a mock implementation of the CarrierAdapter interface
// for DHL Express. Used in tests and when DHL_EXPRESS_USERNAME is not set.
type MockDHLExpressAdapter struct{}

// BookShipment mocks booking a DHL Express shipment.
func (m *MockDHLExpressAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}
	return &BookingResponse{
		ShipmentID:                 "1234567890",
		TrackingNumber:             "1234567890",
		DispatchConfirmationNumber: "PRG200227000256",
		Carrier:                    "dhl_express",
		Status:                     "booked",
		ServiceLevel:               "EXPRESS WORLDWIDE",
		BetaWarning:                "DHL Express integration is in beta — validate in the test environment before going live",
		Colli: []ColliResponse{
			{
				ID:             "JD914600003889482921",
				TrackingNumber: "JD914600003889482921",
				LabelURL:       mockLabelData,
				Status:         "booked",
			},
		},
	}, nil
}

// TrackShipment mocks tracking a DHL Express shipment.
func (m *MockDHLExpressAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		ShipmentID:       trackingNumber,
		TrackingNumber:   trackingNumber,
		Carrier:          "dhl_express",
		Status:           "WC",
		NormalizedStatus: StatusOutForDelivery,
		OriginalStatus:   "WC",
		Events: []TrackingEvent{
			{
				Timestamp:        "2026-06-12T09:00:00",
				Status:           "WC",
				NormalizedStatus: StatusOutForDelivery,
				Location:         "Copenhagen, DK",
				Details:          "With courier",
			},
			{
				Timestamp:        "2026-06-11T14:00:00",
				Status:           "AF",
				NormalizedStatus: StatusInTransit,
				Location:         "Copenhagen, DK",
				Details:          "Arrived at DHL facility",
			},
		},
	}, nil
}

// FetchLabel returns a mock label for DHL Express. PDF, ZPL, and EPL are supported.
func (m *MockDHLExpressAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatZPL, LabelFormatEPL:
		// supported
	default:
		return nil, unsupportedFormat("DHL Express", req.Format, LabelFormatPDF, LabelFormatZPL, LabelFormatEPL)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dhl_express",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for DHL Express.
func (m *MockDHLExpressAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DHL Express", "cancel shipment", "no void AWB endpoint; use DispatchConfirmationNumber to cancel the pickup booking via DELETE /pickups/{id}")
}

// UpdateShipment returns unsupported for DHL Express.
func (m *MockDHLExpressAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL Express", "update shipment", "contact DHL Express customer service")
}
