// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/strategy_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestCarrierAdapterInterface verifies that all mock adapters satisfy the
// CarrierAdapter interface by exercising both methods at compile and run time.
func TestCarrierAdapterInterface(t *testing.T) {
	t.Parallel()

	adapters := []CarrierAdapter{
		&MockPostNordAdapter{},
		&MockBringAdapter{},
		&MockGLSAdapter{},
		&MockDAOAdapter{},
		&MockPostiAdapter{},
		&MockInPostAdapter{},
	}

	request := BookingRequest{
		Carrier: "postnord",
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

	for _, a := range adapters {
		_, err := a.BookShipment(t.Context(), request)
		assert.NoError(t, err)

		_, err = a.TrackShipment(t.Context(), "test-tracking-number")
		assert.NoError(t, err)
	}
}

// TestInitAdapters_StrategySelection verifies that InitAdapters registers all
// expected carriers and that each adapter satisfies the CarrierAdapter interface.
func TestInitAdapters_StrategySelection(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	log := zaptest.NewLogger(t)
	adapters := InitAdapters(log)

	expectedCarriers := []string{
		"postnord",
		"bring",
		"gls",
		"dao",
		"posti",
		"inpost",
	}

	request := BookingRequest{
		Carrier: "postnord",
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
		a, exists := adapters[carrier]
		assert.True(t, exists, "adapter for %s should exist", carrier)
		require.NotNil(t, a, "adapter for %s should not be nil", carrier)

		_, err := a.BookShipment(t.Context(), request)
		assert.NoError(t, err, "BookShipment should not fail for %s", carrier)

		_, err = a.TrackShipment(t.Context(), "test-tracking-number")
		assert.NoError(t, err, "TrackShipment should not fail for %s", carrier)
	}
}

// TestUseAdapter_StrategyExecution verifies the PostNord adapter end-to-end
// through the InitAdapters map.
func TestUseAdapter_StrategyExecution(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	log := zaptest.NewLogger(t)
	adapters := InitAdapters(log)

	a, exists := adapters["postnord"]
	require.True(t, exists)
	require.NotNil(t, a)

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

	bookingResponse, err := a.BookShipment(t.Context(), request)
	require.NoError(t, err)
	require.NotNil(t, bookingResponse)
	assert.Equal(t, "postnord", bookingResponse.Carrier)

	trackingResponse, err := a.TrackShipment(t.Context(), "test-tracking-number")
	require.NoError(t, err)
	assert.NotNil(t, trackingResponse)
}
