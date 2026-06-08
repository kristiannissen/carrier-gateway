// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_dao.go.
package adapter

import (
	"context"
	"fmt"
	"strings"
)

// MockDAOAdapter is a mock implementation of the CarrierAdapter interface for DAO.
type MockDAOAdapter struct{}

// BookShipment mocks booking a shipment with DAO.
func (a *MockDAOAdapter) BookShipment(_ context.Context, request BookingRequest) (*BookingResponse, error) {
	if request.Shipment.TotalWeight <= 0 {
		return nil, fmt.Errorf("TotalWeight is required and must be greater than 0")
	}

	var sumColliWeight float64
	for _, colli := range request.Shipment.Colli {
		sumColliWeight += colli.Weight
	}

	if request.Shipment.TotalWeight != sumColliWeight {
		return nil, fmt.Errorf("TotalWeight must match the sum of all colli weights")
	}

	if hasAddOn(request.Shipment.AddOns, AddOnFlexDelivery) {
		return nil, fmt.Errorf("DAO does not support flex delivery")
	}

	result := &BookingResponse{
		TrackingNumber: "DAO123456789DK",
		Carrier:        "dao",
	}

	// For labelless returns, include the labelless code on the colli response.
	if strings.EqualFold(request.Shipment.DeliveryType, "return") &&
		!strings.EqualFold(request.Shipment.ReturnFunctionality, "withlabel") {
		result.Colli = []ColliResponse{
			{ID: "DAO123456789DK", TrackingNumber: "DAO123456789DK", LabelURL: "123 456 789", Status: "booked"},
		}
	}

	return result, nil
}

// FetchLabel returns a mock label response for DAO.
func (a *MockDAOAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("DAO", req.Format, LabelFormatPDF)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dao",
		Format:         LabelFormatPDF,
		Data:           mockLabelData,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment returns a mock cancel response for DAO.
func (a *MockDAOAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	return &CancelResponse{TrackingNumber: trackingNumber, Carrier: "dao", Status: "cancelled"}, nil
}

// UpdateShipment returns a mock update response for DAO.
func (a *MockDAOAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	var fields []string
	if req.ReceiverPhone != "" {
		fields = append(fields, "phone")
	}
	if req.ReceiverEmail != "" {
		fields = append(fields, "email")
	}
	if req.Weight > 0 {
		fields = append(fields, "weight")
	}
	if req.ServicePointID != "" {
		fields = append(fields, "servicePointId")
	}
	return &UpdateResponse{TrackingNumber: req.TrackingNumber, Carrier: "dao", Status: "updated", UpdatedFields: fields}, nil
}

// TrackShipment mocks tracking a shipment with DAO.
func (a *MockDAOAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dao",
		Status:         "Pakke modtaget på fordelingscenter",
		Events: []TrackingEvent{
			{
				Timestamp: "2026-05-31T12:00:00Z",
				Status:    "10",
				Location:  "DAO Erritsø",
				Details:   "Pakke modtaget på fordelingscenter",
			},
		},
	}, nil
}
