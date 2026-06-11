// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_fedex.go.
package adapter

import (
	"context"
	"fmt"
)

// MockFedExAdapter is a mock implementation of the CarrierAdapter interface for FedEx.
// It returns fixture data and does not contact the FedEx API.
type MockFedExAdapter struct{}

// BookShipment mocks booking a FedEx shipment.
func (m *MockFedExAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}
	return &BookingResponse{
		TrackingNumber: "794644792798",
		Carrier:        "fedex",
		Status:         "booked",
		Colli: []ColliResponse{
			{
				ID:             "794644792798",
				TrackingNumber: "794644792798",
				LabelURL:       "data:application/pdf;base64," + mockLabelData,
				Status:         "booked",
			},
		},
	}, nil
}

// TrackShipment mocks tracking a FedEx shipment.
func (m *MockFedExAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "fedex",
		Status:           "IT",
		NormalizedStatus: StatusInTransit,
		OriginalStatus:   "IT",
		Events: []TrackingEvent{
			{
				Timestamp:        "2026-06-11T08:00:00Z",
				Status:           "PU",
				NormalizedStatus: StatusPickedUp,
				Location:         "Memphis, TN",
				Details:          "Picked up",
			},
		},
	}, nil
}

// FetchLabel returns a mock label response for FedEx.
func (m *MockFedExAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("FedEx", req.Format, LabelFormatPDF)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "fedex",
		Format:         LabelFormatPDF,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment mocks cancellation of a FedEx shipment.
func (m *MockFedExAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "fedex",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment returns unsupported for FedEx.
func (m *MockFedExAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("FedEx", "post-booking update", "")
}
