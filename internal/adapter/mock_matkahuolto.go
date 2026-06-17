// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_matkahuolto.go.
package adapter

import (
	"context"
	"fmt"
)

// MockMatkahuoltoAdapter is a mock implementation of CarrierAdapter for Matkahuolto.
type MockMatkahuoltoAdapter struct{}

// BookShipment mocks booking a shipment with Matkahuolto.
func (a *MockMatkahuoltoAdapter) BookShipment(_ context.Context, req BookingRequest) (*BookingResponse, error) {
	if req.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}
	trackingNumber := "MH123456789FI"
	return &BookingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "matkahuolto",
		Status:         "booked",
		Colli: []ColliResponse{{
			ID:             trackingNumber,
			TrackingNumber: trackingNumber,
			LabelURL:       mockLabelData,
			Status:         "booked",
		}},
	}, nil
}

// TrackShipment mocks tracking a shipment with Matkahuolto.
func (a *MockMatkahuoltoAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "matkahuolto",
		Status:           "35",
		NormalizedStatus: StatusInTransit,
		OriginalStatus:   "35",
		Events: []TrackingEvent{
			{
				Timestamp:        "2026-06-15T10:00:00+03:00",
				Status:           "35",
				NormalizedStatus: StatusInTransit,
				Location:         "HELSINKI",
				Details:          "Received at destination terminal",
			},
			{
				Timestamp:        "2026-06-14T08:30:00+03:00",
				Status:           "08",
				NormalizedStatus: StatusPickedUp,
				Location:         "TAMPERE",
				Details:          "Picked up from sender",
			},
		},
	}, nil
}

// FetchLabel returns a mock label for Matkahuolto. Only PDF format is supported.
func (a *MockMatkahuoltoAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF && req.Format != "" {
		return nil, unsupportedFormat("Matkahuolto", req.Format, LabelFormatPDF)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "matkahuolto",
		Format:         LabelFormatPDF,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment mocks cancelling a shipment with Matkahuolto.
func (a *MockMatkahuoltoAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "matkahuolto",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported by Matkahuolto.
func (a *MockMatkahuoltoAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, ErrNotSupported
}
