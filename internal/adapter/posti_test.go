// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/posti_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockPostiAdapter_BookShipment(t *testing.T) {
	adapter := &MockPostiAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "posti",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Helsinki",
				PostalCode: "00100",
				Country:    "FI",
				Phone:      "+35812345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Tampere",
				PostalCode: "33100",
				Country:    "FI",
				Phone:      "+35887654321",
				Email:      "receiver@example.com",
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
		Carrier: "posti",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Helsinki",
				PostalCode: "00100",
				Country:    "FI",
				Phone:      "+35812345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Tampere",
				PostalCode: "33100",
				Country:    "FI",
				Phone:      "+35887654321",
				Email:      "receiver@example.com",
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
		Carrier: "posti",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Helsinki",
				PostalCode: "00100",
				Country:    "FI",
				Phone:      "+35812345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Tampere",
				PostalCode: "33100",
				Country:    "FI",
				Phone:      "+35887654321",
				Email:      "receiver@example.com",
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
	assert.Equal(t, "POSTI123456789FI", response.TrackingNumber)
	assert.Equal(t, "https://example.com/mock-posti-label.png", response.LabelURL)
}

func TestMockPostiAdapter_TrackShipment(t *testing.T) {
	adapter := &MockPostiAdapter{}

	response, err := adapter.TrackShipment("POSTI123456789FI")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "POSTI123456789FI", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestMockPostiAdapter_GetServicePoints(t *testing.T) {
	adapter := &MockPostiAdapter{}

	location := Location{
		City:       "Helsinki",
		Country:    "FI",
		PostalCode: "00100",
	}
	servicePoints, err := adapter.GetServicePoints(location)
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "POSTI001", servicePoints[0].ID)
}
