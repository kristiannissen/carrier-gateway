// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dao_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockDAOAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	adapter := &MockDAOAdapter{}

	// Test case: TotalWeight is missing
	request := BookingRequest{
		Carrier: "dao",
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
	_, err := adapter.BookShipment(t.Context(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")

	// Test case: TotalWeight does not match sum of colli weights
	request = BookingRequest{
		Carrier: "dao",
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
	_, err = adapter.BookShipment(t.Context(), request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")

	// Test case: TotalWeight is correct
	request = BookingRequest{
		Carrier: "dao",
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
	response, err := adapter.BookShipment(t.Context(), request)
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "DAO123456789DK", response.TrackingNumber)
	assert.Empty(t, response.LabelURL) // DAO no longer returns a label URL — use FetchLabel instead
}

func TestDAOAdapter_BookShipment_AddOnWarnings(t *testing.T) {
	t.Parallel()

	t.Run("contact update failure surfaces AddOnWarning not error", func(t *testing.T) {
		t.Parallel()

		// Server returns success for booking but failure for contact update.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "OpdaterKontaktOplysning") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"FEJL","fejlkode":"42","fejltekst":"address not found"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"OK","resultat":{"stregkode":"00057126960000003016","labelTekst1":"123"}}`))
		}))
		t.Cleanup(srv.Close)

		adapter := &DAOAdapter{
			CustomerID: "123456",
			APIKey:     "testkey",
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		}

		resp, err := adapter.BookShipment(t.Context(), BookingRequest{
			Carrier: "dao",
			Shipment: Shipment{
				Sender: Address{
					Name: "Sender", Street: "Vejnavn", City: "Copenhagen",
					PostalCode: "2300", Country: "DK",
				},
				Receiver: Address{
					Name: "Receiver", Street: "Modtagervej", City: "Aarhus",
					PostalCode: "8000", Country: "DK",
					Phone: "+4587654321", Email: "receiver@example.com",
				},
				TotalWeight: 1.0,
				Colli:       []Colli{{ID: "c1", Weight: 1.0}},
				AddOns:      []AddOn{{Type: AddOnSMSNotification}},
			},
		})

		// Booking must succeed — the parcel is created.
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "00057126960000003016", resp.TrackingNumber)

		// AddOnWarning must be present — not an error, not silent.
		require.Len(t, resp.AddOnWarnings, 1)
		assert.Contains(t, resp.AddOnWarnings[0], "sms_notification/email_notification")
		assert.Contains(t, resp.AddOnWarnings[0], "address not found")
		assert.Contains(t, resp.AddOnWarnings[0], "00057126960000003016")
		assert.Contains(t, resp.AddOnWarnings[0], "PATCH /api/bookings/00057126960000003016?carrier=dao")
	})

	t.Run("successful contact update produces no warnings", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if strings.Contains(r.URL.Path, "OpdaterKontaktOplysning") {
				_, _ = w.Write([]byte(`{"status":"OK"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"OK","resultat":{"stregkode":"00057126960000003016"}}`))
		}))
		t.Cleanup(srv.Close)

		adapter := &DAOAdapter{
			CustomerID: "123456",
			APIKey:     "testkey",
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		}

		resp, err := adapter.BookShipment(t.Context(), BookingRequest{
			Carrier: "dao",
			Shipment: Shipment{
				Sender:      Address{Name: "S", Street: "V", City: "C", PostalCode: "2300", Country: "DK"},
				Receiver:    Address{Name: "R", Street: "V", City: "A", PostalCode: "8000", Country: "DK", Phone: "+4587654321", Email: "r@x.com"},
				TotalWeight: 1.0,
				Colli:       []Colli{{ID: "c1", Weight: 1.0}},
				AddOns:      []AddOn{{Type: AddOnSMSNotification}},
			},
		})

		require.NoError(t, err)
		assert.Empty(t, resp.AddOnWarnings)
	})

	t.Run("booking without add-ons produces no warnings", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"OK","resultat":{"stregkode":"00057126960000003016"}}`))
		}))
		t.Cleanup(srv.Close)

		adapter := &DAOAdapter{
			CustomerID: "123456",
			APIKey:     "testkey",
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		}

		resp, err := adapter.BookShipment(t.Context(), BookingRequest{
			Carrier: "dao",
			Shipment: Shipment{
				Sender:      Address{Name: "S", Street: "V", City: "C", PostalCode: "2300", Country: "DK"},
				Receiver:    Address{Name: "R", Street: "V", City: "A", PostalCode: "8000", Country: "DK"},
				TotalWeight: 1.0,
				Colli:       []Colli{{ID: "c1", Weight: 1.0}},
			},
		})

		require.NoError(t, err)
		assert.Empty(t, resp.AddOnWarnings)
	})
}

func TestMockDAOAdapter_TrackShipment(t *testing.T) {
	t.Parallel()
	adapter := &MockDAOAdapter{}

	response, err := adapter.TrackShipment(t.Context(), "DAO123456789DK")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "DAO123456789DK", response.TrackingNumber)
	assert.Equal(t, "10", response.Status)
	assert.Equal(t, StatusInTransit, response.NormalizedStatus)
	assert.Len(t, response.Events, 1)
}
