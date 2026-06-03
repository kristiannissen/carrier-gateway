// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/postnord_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockPostNordAdapter_BookShipment(t *testing.T) {
	// 
	t.Parallel()

	adapter := &MockPostNordAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "12345",
				Country:    "DK",
				Phone:      "+4512345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Receiver City",
				PostalCode: "67890",
				Country:    "DK",
				Phone:      "+4587654321",
				Email:      "receiver@example.com",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 10.0,
						Width:  10.0,
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
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "12345",
				Country:    "DK",
				Phone:      "+4512345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Receiver City",
				PostalCode: "67890",
				Country:    "DK",
				Phone:      "+4587654321",
				Email:      "receiver@example.com",
			},
			TotalWeight: 5.0, // Mismatched with colli weight (10.0)
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 10.0,
						Width:  10.0,
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
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "12345",
				Country:    "DK",
				Phone:      "+4512345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Receiver City",
				PostalCode: "67890",
				Country:    "DK",
				Phone:      "+4587654321",
				Email:      "receiver@example.com",
			},
			TotalWeight: 10.0, // Matches sum of colli weights
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 10.0,
						Width:  10.0,
						Height: 10.0,
					},
				},
			},
		},
	}
	response, err := adapter.BookShipment(request)
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "postnord", response.Carrier)
	assert.NotEmpty(t, response.TrackingNumber)
	assert.NotEmpty(t, response.LabelURL)
}

func TestMockPostNordAdapter_TrackShipment(t *testing.T) {
	t.Parallel()
	adapter := &MockPostNordAdapter{}

	response, err := adapter.TrackShipment("PN123456789DK")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "PN123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 2)
}

func TestMockPostNordAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()
	adapter := &MockPostNordAdapter{}

	location := Location{
		City:       "Copenhagen",
		Country:    "DK",
		PostalCode: "12345",
	}
	servicePoints, err := adapter.GetServicePoints(location)
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 2)
	assert.Equal(t, "sp_123", servicePoints[0].ID)
	assert.Equal(t, "PostNord Copenhagen", servicePoints[0].Name)
}
