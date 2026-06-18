// Package adapter provides a mock DPD NL CarrierAdapter for testing and local development.
// This file is located at /internal/adapter/mock_dpd_nl.go.
package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// MockDPDNLAdapter implements CarrierAdapter with pre-canned DPD NL responses.
// Override individual methods via the corresponding Func fields:
//
//	a := &MockDPDNLAdapter{
//	    BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
//	        return nil, errors.New("upstream timeout")
//	    },
//	}
type MockDPDNLAdapter struct {
	BookShipmentFunc  func(req BookingRequest) (*BookingResponse, error)
	TrackShipmentFunc func(trackingNumber string) (*TrackingResponse, error)
}

// BookShipment returns a mock DPD NL booking response.
// TrackingNumber is a synthetic 14-digit parcel number.
func (m *MockDPDNLAdapter) BookShipment(_ context.Context, req BookingRequest) (*BookingResponse, error) {
	if m.BookShipmentFunc != nil {
		return m.BookShipmentFunc(req)
	}
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	shipmentID := fmt.Sprintf("MPS0522%016d", rand.Intn(1000000000)) //nolint:gosec // mock data
	colli := make([]ColliResponse, len(req.Shipment.Colli))
	firstParcel := ""
	for i, c := range req.Shipment.Colli {
		pn := fmt.Sprintf("%014d", rand.Intn(100000000)) //nolint:gosec // mock data
		if i == 0 {
			firstParcel = pn
		}
		colli[i] = ColliResponse{ID: c.ID, TrackingNumber: pn, Status: "booked"}
	}

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: firstParcel,
		Carrier:        "dpd_nl",
		Status:         "booked",
		Colli:          colli,
		BetaWarning:    "dpd_nl adapter is in beta — validate in sandbox before production use",
	}, nil
}

// TrackShipment returns a mock DPD NL tracking response with one canned event.
func (m *MockDPDNLAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	if m.TrackShipmentFunc != nil {
		return m.TrackShipmentFunc(trackingNumber)
	}

	events := []TrackingEvent{
		{
			Timestamp:        time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			Status:           "IN_TRANSIT",
			NormalizedStatus: StatusInTransit,
			Location:         "Amsterdam, NL",
			Details:          "Parcel in transit",
		},
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "dpd_nl",
		Status:           "IN_TRANSIT",
		NormalizedStatus: StatusInTransit,
		OriginalStatus:   "IN_TRANSIT",
		Events:           events,
	}, nil
}

// FetchLabel returns a mock label response.
func (m *MockDPDNLAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd_nl",
		Format:         req.Format,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment is not supported by DPD NL.
func (m *MockDPDNLAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DPD NL", "cancellation", "contact DPD customer service before 22:00 on the booking day")
}

// UpdateShipment is not supported by DPD NL.
func (m *MockDPDNLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD NL", "post-booking update", "cancel and rebook")
}

// NewMockDPDNLAdapter returns a MockDPDNLAdapter with default behaviour.
func NewMockDPDNLAdapter() *MockDPDNLAdapter {
	return &MockDPDNLAdapter{}
}
