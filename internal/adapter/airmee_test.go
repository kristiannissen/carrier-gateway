// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/airmee_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockAirmeeAdapter_BookShipment(t *testing.T) {
	// Test parallel
	t.Parallel()

	adapter := &MockAirmeeAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "airmee",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Copenhagen",
				PostalCode: "5678",
				Country:    "DK",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 30.0,
						Width:  20.0,
						Height: 15.0,
					},
				},
			},
		},
	}
	_, err := adapter.BookShipment(t.Context(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")

	// Test case: TotalWeight does not match sum of colli weights
	request = BookingRequest{
		Carrier: "airmee",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Copenhagen",
				PostalCode: "5678",
				Country:    "DK",
			},
			TotalWeight: 5.0, // Mismatched with colli weight (10.0)
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 30.0,
						Width:  20.0,
						Height: 15.0,
					},
				},
			},
		},
	}
	_, err = adapter.BookShipment(t.Context(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")

	// Test case: TotalWeight is correct
	request = BookingRequest{
		Carrier: "airmee",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Copenhagen",
				PostalCode: "5678",
				Country:    "DK",
			},
			TotalWeight: 10.0, // Matches sum of colli weights
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 30.0,
						Width:  20.0,
						Height: 15.0,
					},
				},
			},
		},
	}
	response, err := adapter.BookShipment(t.Context(), request)
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "AIRMEE123456789", response.TrackingNumber)
	assert.Equal(t, "https://example.com/mock-airmee-tracking", response.LabelURL)
}

func TestMockAirmeeAdapter_TrackShipment(t *testing.T) {
	t.Parallel()
	adapter := &MockAirmeeAdapter{}

	response, err := adapter.TrackShipment(t.Context(), "AIRMEE123456789")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "AIRMEE123456789", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 2)
}

func TestMockAirmeeAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()
	adapter := &MockAirmeeAdapter{}

	location := Location{
		City:    "Copenhagen",
		Country: "DK",
	}
	servicePoints, err := adapter.GetServicePoints(t.Context(), location)
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 0)
}
