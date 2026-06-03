// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/strategy_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test that all adapters implement the CarrierAdapter interface.
// This ensures the Strategy Pattern is correctly applied.
func TestCarrierAdapterInterface(t *testing.T) {
	// Create a slice of CarrierAdapter interfaces
	adapters := []CarrierAdapter{
		&MockPostNordAdapter{},
		&MockBringAdapter{},
		&MockGLSAdapter{},
		&MockDAOAdapter{},
		&MockPostiAdapter{},
		&MockAirmeeAdapter{},
	}

	bookingRequest := BookingRequest{
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
			TotalWeight: 10.0,
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
	// Verify that each adapter implements the CarrierAdapter interface
	// by calling all methods. This will compile-time check the interface.
	for _, adapter := range adapters {
		// Test BookShipment
		_, err := adapter.BookShipment(bookingRequest)
		assert.NoError(t, err, "BookShipment method should not panic")

		// Test TrackShipment
		_, err = adapter.TrackShipment("test-tracking-number")
		assert.NoError(t, err, "TrackShipment method should not panic")

		// Test GetServicePoints
		_, err = adapter.GetServicePoints(Location{})
		assert.NoError(t, err, "GetServicePoints method should not panic")
	}
}

// Test that InitAdapters returns the correct adapter for each carrier.
func TestInitAdapters_StrategySelection(t *testing.T) {
	// Set mock mode to ensure all adapters are initialized as mocks
	t.Setenv("MOCK_MODE", "true")

	// Initialize adapters
	adapters := InitAdapters()

	// Verify that all expected carriers are present
	expectedCarriers := []string{
		"postnord",
		"bring",
		"gls",
		"dao",
		"posti",
		"airmee",
	}

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
			TotalWeight: 10.0,
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
	for _, carrier := range expectedCarriers {
		adapter, exists := adapters[carrier]
		assert.True(t, exists, "Adapter for carrier %s should exist", carrier)
		assert.NotNil(t, adapter, "Adapter for carrier %s should not be nil", carrier)

		// Verify that the adapter implements CarrierAdapter
		_, err := adapter.BookShipment(request)
		assert.NoError(t, err, "BookShipment should not panic for %s", carrier)

		_, err = adapter.TrackShipment("test-tracking-number")
		assert.NoError(t, err, "TrackShipment should not panic for %s", carrier)

		_, err = adapter.GetServicePoints(Location{})
		assert.NoError(t, err, "GetServicePoints should not panic for %s", carrier)
	}
}

// Test that the correct adapter is used for a specific carrier.
func TestUseAdapter_StrategyExecution(t *testing.T) {
	// Set mock mode to ensure all adapters are initialized as mocks
	t.Setenv("MOCK_MODE", "true")

	// Initialize adapters
	adapters := InitAdapters()

	// Test PostNord adapter
	postNordAdapter, exists := adapters["postnord"]
	assert.True(t, exists, "PostNord adapter should exist")
	assert.NotNil(t, postNordAdapter, "PostNord adapter should not be nil")

	// Create a test request
	request := BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Test Sender",
				Street:     "Test Street",
				City:       "Test City",
				PostalCode: "1234",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Test Receiver",
				Street:     "Test Receiver Street",
				City:       "Test Receiver City",
				PostalCode: "5678",
				Country:    "DK",
			},
			TotalWeight: 5.0,
			Colli: []Colli{
				{
					ID:     "test-colli",
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

	// Test BookShipment
	bookingResponse, err := postNordAdapter.BookShipment(request)
	assert.NoError(t, err, "BookShipment should not fail for PostNord")
	assert.NotNil(t, bookingResponse, "BookingResponse should not be nil")
	assert.Equal(t, "postnord", bookingResponse.Carrier, "Carrier should be postnord")

	// Test TrackShipment
	trackingResponse, err := postNordAdapter.TrackShipment("test-tracking-number")
	assert.NoError(t, err, "TrackShipment should not fail for PostNord")
	assert.NotNil(t, trackingResponse, "TrackingResponse should not be nil")

	// Test GetServicePoints
	servicePoints, err := postNordAdapter.GetServicePoints(Location{
		City:    "Test City",
		Country: "DK",
	})
	assert.NoError(t, err, "GetServicePoints should not fail for PostNord")
	assert.NotNil(t, servicePoints, "ServicePoints should not be nil")

	// Repeat for Airmee (a different type of carrier)
	airmeeAdapter, exists := adapters["airmee"]
	assert.True(t, exists, "Airmee adapter should exist")
	assert.NotNil(t, airmeeAdapter, "Airmee adapter should not be nil")

	// Test BookShipment for Airmee
	airmeeBookingResponse, err := airmeeAdapter.BookShipment(request)
	assert.NoError(t, err, "BookShipment should not fail for Airmee")
	assert.NotNil(t, airmeeBookingResponse, "BookingResponse should not be nil")
	assert.Equal(t, "airmee", airmeeBookingResponse.Carrier, "Carrier should be airmee")
}
