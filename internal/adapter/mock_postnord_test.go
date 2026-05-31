// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/mock_postnord_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockPostNordAdapter_BookShipment(t *testing.T) {
	// Initialize mock adapter
	adapter := &MockPostNordAdapter{}

	// Test booking request
	request := BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:   "Sender Name",
				Street: "Sender Street",
				City:   "Sender City",
				PostalCode: "12345",
				Country: "DK",
			},
			Receiver: Address{
				Name:   "Receiver Name",
				Street: "Receiver Street",
				City:   "Receiver City",
				PostalCode: "67890",
				Country: "DK",
			},
			Colli: []Colli{
				{
					ID:       "colli-1",
					Weight:   10.0,
					Dimensions: Dimensions{
						Length: 10.0,
						Width:  10.0,
						Height: 10.0,
					},
				},
			},
		},
	}

	// Call the BookShipment method
	response, err := adapter.BookShipment(request)

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "PN203738611DK", response.TrackingNumber)
	assert.Equal(t, "https://mock.postnord.com/labels/PN910234148DK.pdf", response.LabelURL)
}

func TestMockPostNordAdapter_TrackShipment(t *testing.T) {
	// Initialize mock adapter
	adapter := &MockPostNordAdapter{}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("PN203738611DK")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "PN203738611DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
}
