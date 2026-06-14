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
		&MockDHLAdapter{},
		&MockInPostAdapter{},
		&MockFedExAdapter{},
		&MockDPDAdapter{},
	}

	request := minimalBookingRequest("postnord")

	for _, a := range adapters {
		_, err := a.BookShipment(t.Context(), request)
		assert.NoError(t, err)

		_, err = a.TrackShipment(t.Context(), "test-tracking-number")
		assert.NoError(t, err)
	}
}

// TestRegistry_Select verifies that Select returns the correct adapter for
// each registered carrier and an error for an unknown carrier.
func TestRegistry_Select(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	registry := NewRegistry(zaptest.NewLogger(t))

	t.Run("known carriers return an adapter", func(t *testing.T) {
		t.Parallel()
		for _, carrier := range []string{"postnord", "bring", "gls", "dao", "dhl", "inpost", "fedex"} {
			a, err := registry.Select(carrier)
			require.NoErrorf(t, err, "Select(%q) should not error", carrier)
			assert.NotNilf(t, a, "Select(%q) should return a non-nil adapter", carrier)
		}
	})

	t.Run("unknown carrier returns error", func(t *testing.T) {
		t.Parallel()
		_, err := registry.Select("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
	})
}

// TestRegistry_Carriers verifies that Carriers returns all registered keys.
func TestRegistry_Carriers(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	registry := NewRegistry(zaptest.NewLogger(t))
	carriers := registry.Carriers()

	assert.ElementsMatch(t, []string{"postnord", "bring", "gls", "dao", "dhl", "inpost", "fedex", "hermes", "dhl_express", "omniva", "dpd_uk"}, carriers)
}

// TestRegistry_StrategyExecution verifies that selecting a carrier and calling
// its methods produces the expected response — the strategy is executed, not
// just selected.
func TestRegistry_StrategyExecution(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	registry := NewRegistry(zaptest.NewLogger(t))

	carriers := []string{"postnord", "bring", "gls", "dao", "dhl", "inpost", "fedex"}

	for _, carrier := range carriers {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()

			a, err := registry.Select(carrier)
			require.NoError(t, err)

			req := minimalBookingRequest(carrier)
			resp, err := a.BookShipment(t.Context(), req)
			require.NoErrorf(t, err, "BookShipment failed for %s", carrier)
			require.NotNilf(t, resp, "BookShipment returned nil for %s", carrier)
			assert.Equalf(t, carrier, resp.Carrier, "unexpected carrier in response for %s", carrier)

			trackResp, err := a.TrackShipment(t.Context(), "test-tracking-number")
			require.NoErrorf(t, err, "TrackShipment failed for %s", carrier)
			assert.NotNilf(t, trackResp, "TrackShipment returned nil for %s", carrier)
		})
	}
}

// TestRegistry_SwitchingCarriers verifies the core strategy guarantee: the
// same request struct with a different Carrier field routes to a different
// adapter and returns a carrier-specific response.
func TestRegistry_SwitchingCarriers(t *testing.T) {
	t.Setenv("MOCK_MODE", "true")

	registry := NewRegistry(zaptest.NewLogger(t))

	for _, carrier := range []string{"postnord", "bring", "gls", "dao", "dhl", "inpost", "fedex"} {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()

			a, err := registry.Select(carrier)
			require.NoError(t, err)

			req := minimalBookingRequest(carrier)
			resp, err := a.BookShipment(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, carrier, resp.Carrier,
				"response carrier must match the selected adapter for %s", carrier)
		})
	}
}

// TestRegisterDPD verifies that registerDPD registers one adapter per
// DPD_{COUNTRY}_API_TOKEN + DPD_{COUNTRY}_BASE_URL pair found in env.
// Subtests use t.Setenv and cannot be parallel.
func TestRegisterDPD(t *testing.T) {
	t.Run("registers dpd_lt when env vars are set", func(t *testing.T) {
		t.Setenv("DPD_LT_API_TOKEN", "test-token")
		t.Setenv("DPD_LT_BASE_URL", "https://esiunta.dpd.lt/api/v1")

		adapters := make(map[string]CarrierAdapter)
		registerDPD(adapters, false, zaptest.NewLogger(t))

		a, ok := adapters["dpd_lt"]
		require.True(t, ok, "dpd_lt should be registered")
		assert.NotNil(t, a)
	})

	t.Run("falls back to mock when BASE_URL missing", func(t *testing.T) {
		t.Setenv("DPD_BE_API_TOKEN", "test-token")
		// DPD_BE_BASE_URL intentionally not set.

		adapters := make(map[string]CarrierAdapter)
		registerDPD(adapters, false, zaptest.NewLogger(t))

		a, ok := adapters["dpd_be"]
		require.True(t, ok, "dpd_be should be registered (as mock)")
		assert.IsType(t, &MockDPDAdapter{}, a)
	})

	t.Run("no adapters registered when no DPD env vars", func(t *testing.T) {
		adapters := make(map[string]CarrierAdapter)
		registerDPD(adapters, false, zaptest.NewLogger(t))
		assert.Empty(t, adapters)
	})

	t.Run("mock mode registers mock regardless of token", func(t *testing.T) {
		t.Setenv("DPD_AT_API_TOKEN", "real-token")
		t.Setenv("DPD_AT_BASE_URL", "https://...dpd.at/api/v1")

		adapters := make(map[string]CarrierAdapter)
		registerDPD(adapters, true, zaptest.NewLogger(t))

		a, ok := adapters["dpd_at"]
		require.True(t, ok)
		assert.IsType(t, &MockDPDAdapter{}, a)
	})
}

// minimalBookingRequest returns the smallest valid BookingRequest for the
// given carrier. Used across strategy tests to reduce noise.
func minimalBookingRequest(carrier string) BookingRequest {
	return BookingRequest{
		Carrier: carrier,
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "2300",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Copenhagen",
				PostalCode: "2300",
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
}
