// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dhl.go.
package adapter

import (
	"context"
	"fmt"
)

// MockDHLAdapter is a mock implementation of the CarrierAdapter interface for DHL.
type MockDHLAdapter struct{}

// BookShipment mocks booking a DHL shipment.
func (m *MockDHLAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}
	var sumColliWeight float64
	for _, c := range request.Shipment.Colli {
		sumColliWeight += c.Weight
	}
	if request.Shipment.TotalWeight != sumColliWeight {
		return nil, fmt.Errorf("TotalWeight must match the sum of all colli weights")
	}

	return &BookingResponse{
		TrackingNumber: "JJD14900053379980",
		Carrier:        "dhl",
		Status:         "booked",
		Colli: []ColliResponse{
			{
				ID:             "JJD14900053379980",
				TrackingNumber: "JJD14900053379980",
				LabelURL:       mockLabelData,
				Status:         "booked",
			},
		},
	}, nil
}

// TrackShipment mocks tracking a DHL shipment.
func (m *MockDHLAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "dhl",
		Status:           "transit",
		NormalizedStatus: StatusInTransit,
		OriginalStatus:   "transit",
		Events: []TrackingEvent{
			{
				Timestamp:        "2026-06-07T12:00:00Z",
				Status:           "transit",
				NormalizedStatus: StatusInTransit,
				Location:         "Hamburg, DE",
				Details:          "Shipment is in transit",
			},
		},
	}, nil
}

// FetchLabel returns a mock label for DHL. Mirrors the real adapter's
// supported formats: PDF, PNG, and ZPL.
func (m *MockDHLAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatPNG, LabelFormatZPL:
	default:
		return nil, unsupportedFormat("DHL", req.Format, LabelFormatPDF, LabelFormatPNG, LabelFormatZPL)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dhl",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment returns unsupported for DHL — mirrors DHLAdapter.CancelShipment.
func (m *MockDHLAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DHL", "cancellation",
		"eConnect has no cancel/void endpoint; if not yet collected by DHL, discard the label and book a corrected shipment instead — if already collected, contact DHL customer service")
}

// UpdateShipment returns unsupported for DHL — mirrors DHLAdapter.UpdateShipment.
func (m *MockDHLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL", "post-booking update",
		"eConnect has no update/patch endpoint; submit a new booking request with corrected data and discard the old label")
}
