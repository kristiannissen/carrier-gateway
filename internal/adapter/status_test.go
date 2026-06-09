// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/status_test.go.
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeStatus(t *testing.T) {
	t.Parallel()

	t.Run("postnord", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			raw  string
			want TrackingStatus
		}{
			// Confirmed from live production test.
			{"INFORMED", StatusBooked},
			// Inferred from PostNord status progression.
			// TODO: Confirm against the full PostNord v5 status enum —
			// contact kundeintegration@postnord.com or check developer.postnord.com.
			{"AVAILABLE_FOR_DELIVERY", StatusInTransit},
			{"EXPECTED_DELIVERY", StatusInTransit},
			{"EN_ROUTE", StatusInTransit},
			{"IN_TRANSPORT", StatusInTransit},
			{"AT_DISTRIBUTION_CENTER", StatusInTransit},
			{"OUT_FOR_DELIVERY", StatusOutForDelivery},
			{"DELIVERED", StatusDelivered},
			{"DELIVERY_IMPOSSIBLE", StatusFailed},
			{"FAILED_DELIVERY", StatusFailed},
			{"RETURN_TO_SENDER", StatusReturned},
			{"RETURNED", StatusReturned},
			{"DELAYED", StatusDelayed},
			// Fallbacks
			{"", StatusUnknown},
			{"UNKNOWN_STATUS_XYZ", StatusUnknown},
		}
		for _, c := range cases {
			c := c
			t.Run(c.raw, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, c.want, normalizeStatus("postnord", c.raw))
			})
		}
	})

	t.Run("bring", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			raw  string
			want TrackingStatus
		}{
			// Full enum from Bring Tracking API YAML spec.
			{"ATTEMPTED_DELIVERY", StatusFailed},
			{"COLLECTED", StatusPickedUp},
			{"CUSTOMS", StatusInTransit},
			{"DELIVERED", StatusDelivered},
			{"DELIVERED_SENDER", StatusReturned},
			{"DELIVERY_CANCELLED", StatusFailed},
			{"DELIVERY_CHANGED", StatusInTransit},
			{"DELIVERY_ORDERED", StatusBooked},
			{"DEVIATION", StatusFailed},
			{"HANDED_IN", StatusPickedUp},
			{"INTERNATIONAL", StatusInTransit},
			{"IN_TRANSIT", StatusInTransit},
			{"NOTIFICATION_SENT", StatusBooked},
			{"PRE_NOTIFIED", StatusBooked},
			{"READY_FOR_PICKUP", StatusOutForDelivery},
			{"RETURN", StatusReturned},
			{"TERMINAL", StatusInTransit},
			{"TRANSPORT_TO_RECIPIENT", StatusOutForDelivery},
			{"UNKNOWN", StatusUnknown},
			// Fallbacks
			{"", StatusUnknown},
			{"NOT_A_REAL_STATUS", StatusUnknown},
		}
		for _, c := range cases {
			c := c
			t.Run(c.raw, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, c.want, normalizeStatus("bring", c.raw))
			})
		}
	})

	t.Run("gls", func(t *testing.T) {
		t.Parallel()

		// TODO: GLS does not publish its StatusCode enum publicly.
		// All GLS statuses resolve to StatusUnknown until the full list is
		// obtained from GLS support or observed from live shipment events.
		cases := []struct {
			raw  string
			want TrackingStatus
		}{
			{"", StatusUnknown},
			{"SOME_GLS_CODE", StatusUnknown},
			{"DELIVERED", StatusUnknown}, // GLS key is not "gls"/"delivered" — unknown until confirmed
		}
		for _, c := range cases {
			c := c
			t.Run(c.raw, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, c.want, normalizeStatus("gls", c.raw))
			})
		}
	})

	t.Run("dao", func(t *testing.T) {
		t.Parallel()

		// Numeric event codes from DAO TrackNTrace_v2.php haendelse field.
		// TODO: Cross-check the full list against GET /TrackNTraceKoder.php.
		cases := []struct {
			raw  string
			want TrackingStatus
		}{
			{"10", StatusInTransit},      // Pakke modtaget på fordelingscenter
			{"20", StatusInTransit},      // Pakke er ankommet til terminal
			{"30", StatusDelivered},      // Pakke er afleveret
			{"40", StatusFailed},         // Pakke er ikke afleveret
			{"50", StatusReturned},       // Pakke returneret til afsender
			{"60", StatusOutForDelivery}, // Pakke er på vej til modtager
			{"70", StatusBooked},         // Pakke er registreret
			// Fallbacks
			{"", StatusUnknown},
			{"99", StatusUnknown},
		}
		for _, c := range cases {
			c := c
			t.Run(c.raw, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, c.want, normalizeStatus("dao", c.raw))
			})
		}
	})

	t.Run("dhl", func(t *testing.T) {
		t.Parallel()

		// Full enum from DHL Unified Tracking API v1.5.8 spec.
		cases := []struct {
			raw  string
			want TrackingStatus
		}{
			{"delivered", StatusDelivered},
			{"failure", StatusFailed},
			{"pre-transit", StatusBooked},
			{"transit", StatusInTransit},
			{"unknown", StatusUnknown},
			// Fallbacks
			{"", StatusUnknown},
			{"DELIVERED", StatusUnknown}, // DHL enum is lowercase — case-sensitive
			{"not_a_real_status", StatusUnknown},
		}
		for _, c := range cases {
			c := c
			t.Run(c.raw, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, c.want, normalizeStatus("dhl", c.raw))
			})
		}
	})

	t.Run("unknown_carrier", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, StatusUnknown, normalizeStatus("fedex", "DELIVERED"))
		assert.Equal(t, StatusUnknown, normalizeStatus("", "DELIVERED"))
		assert.Equal(t, StatusUnknown, normalizeStatus("posti", "DELIVERED"))
		assert.Equal(t, StatusUnknown, normalizeStatus("inpost", "DELIVERED"))
	})
}
