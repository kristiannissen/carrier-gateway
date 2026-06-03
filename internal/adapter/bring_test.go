// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/bring_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockBringAdapter_BookShipment(t *testing.T) {
	// 
	t.Parallel()

	adapter := &MockBringAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "bring",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Oslo",
				PostalCode: "0123",
				Country:    "NO",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Bergen",
				PostalCode: "5678",
				Country:    "NO",
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
		Carrier: "bring",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Oslo",
				PostalCode: "0123",
				Country:    "NO",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Bergen",
				PostalCode: "5678",
				Country:    "NO",
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
		Carrier: "bring",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Oslo",
				PostalCode: "0123",
				Country:    "NO",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Bergen",
				PostalCode: "5678",
				Country:    "NO",
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
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "https://example.com/mock-bring-label.png", response.LabelURL)
}

func TestMockBringAdapter_TrackShipment(t *testing.T) {
	t.Parallel()
	adapter := &MockBringAdapter{}

	response, err := adapter.TrackShipment("BR123456789NO")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestMockBringAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()
	adapter := &MockBringAdapter{}

	location := Location{
		City:    "Oslo",
		Country: "NO",
	}
	servicePoints, err := adapter.GetServicePoints(location)
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "BR001", servicePoints[0].ID)
}
