// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/gls_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockGLSAdapter_BookShipment(t *testing.T) {
	adapter := &MockGLSAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
				Phone:      "+4512345678",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Aarhus",
				PostalCode: "5678",
				Country:    "DK",
				Phone:      "+4587654321",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 5.0,
					Dimensions: Dimensions{
						Length: 20.0,
						Width:  15.0,
						Height: 10.0,
					},
				},
			},
			// TotalWeight is missing
		},
	}
	_, err := adapter.BookShipment(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")

	// Test case: TotalWeight does not match sum of colli weights
	request = BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
				Phone:      "+4512345678",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Aarhus",
				PostalCode: "5678",
				Country:    "DK",
				Phone:      "+4587654321",
			},
			TotalWeight: 3.0, // Mismatched with colli weight (5.0)
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 5.0,
					Dimensions: Dimensions{
						Length: 20.0,
						Width:  15.0,
						Height: 10.0,
					},
				},
			},
		},
	}
	_, err = adapter.BookShipment(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")

	// Test case: TotalWeight is correct
	request = BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
				Phone:      "+4512345678",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Aarhus",
				PostalCode: "5678",
				Country:    "DK",
				Phone:      "+4587654321",
			},
			TotalWeight: 5.0, // Matches sum of colli weights
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 5.0,
					Dimensions: Dimensions{
						Length: 20.0,
						Width:  15.0,
						Height: 10.0,
					},
				},
			},
		},
	}
	response, err := adapter.BookShipment(request)
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "GLS123456789DK", response.TrackingNumber)
	assert.Equal(t, "https://example.com/mock-gls-label.png", response.LabelURL)
}

func TestMockGLSAdapter_TrackShipment(t *testing.T) {
	adapter := &MockGLSAdapter{}

	response, err := adapter.TrackShipment("GLS123456789DK")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "GLS123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestMockGLSAdapter_GetServicePoints(t *testing.T) {
	adapter := &MockGLSAdapter{}

	location := Location{
		City:    "Copenhagen",
		Country: "DK",
	}
	servicePoints, err := adapter.GetServicePoints(location)
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "GLS001", servicePoints[0].ID)
}
