// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/ufficiopostale_test.go.
package adapter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func upTestAdapter(srv *httptest.Server) *UfficioPostaleAdapter {
	a := NewUfficioPostaleAdapter("test-api-key", false, zap.NewNop())
	a.BaseURL = srv.URL
	a.HTTPClient = srv.Client()
	return a
}

func upTestSender() Address {
	return Address{
		Name:        "Mario Rossi",
		Street:      "Via Roma",
		HouseNumber: "1",
		City:        "Roma",
		PostalCode:  "00100",
		Country:     "IT",
		State:       "RM",
		Email:       "mario@example.com",
	}
}

func upTestReceiver() Address {
	return Address{
		Name:        "Luigi Verdi",
		Street:      "Via Milano",
		HouseNumber: "42",
		City:        "Milano",
		PostalCode:  "20100",
		Country:     "IT",
		State:       "MI",
		Email:       "luigi@example.com",
	}
}

func upSuccessEnvelope(data string) string {
	return `{"success":true,"message":"","data":` + data + `}`
}

// ─── normaliseUfficioPostaleStatus ───────────────────────────────────────────

func TestNormaliseUfficioPostaleStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code string
		want TrackingStatus
	}{
		{"00", StatusBooked},
		{"01", StatusDelivered},
		{"03", StatusFailed},
		{"10", StatusInTransit},
		{"20", StatusOutForDelivery},
		{"30", StatusFailed},
		{"40", StatusFailed},
		{"70", StatusInTransit},
		{"91", StatusFailed},
		{"93", StatusInTransit},
		{"100", StatusBooked},
		{"110", StatusBooked},
		{"999", StatusUnknown},
		{"", StatusUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, normaliseUfficioPostaleStatus(tc.code))
		})
	}
}

// ─── upProductPath ───────────────────────────────────────────────────────────

func TestUpProductPath(t *testing.T) {
	t.Parallel()

	t.Run("empty defaults to raccomandate", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("")
		require.NoError(t, err)
		assert.Equal(t, "raccomandate", p)
	})

	t.Run("raccomandata maps to raccomandate", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("raccomandata")
		require.NoError(t, err)
		assert.Equal(t, "raccomandate", p)
	})

	t.Run("raccomandata_smart maps correctly", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("raccomandata_smart")
		require.NoError(t, err)
		assert.Equal(t, "raccomandate_smart", p)
	})

	t.Run("ordinaria maps to ordinarie", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("ordinaria")
		require.NoError(t, err)
		assert.Equal(t, "ordinarie", p)
	})

	t.Run("atti_giudiziari maps to atti_giudiziari", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("atti_giudiziari")
		require.NoError(t, err)
		assert.Equal(t, "atti_giudiziari", p)
	})

	t.Run("case-insensitive", func(t *testing.T) {
		t.Parallel()
		p, err := upProductPath("ORDINARIA")
		require.NoError(t, err)
		assert.Equal(t, "ordinarie", p)
	})

	t.Run("unsupported tier returns error", func(t *testing.T) {
		t.Parallel()
		_, err := upProductPath("express")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported service tier")
		assert.Contains(t, err.Error(), "express")
	})
}

// ─── upDocumentContent ───────────────────────────────────────────────────────

func TestUpDocumentContent(t *testing.T) {
	t.Parallel()

	t.Run("ShipmentComment takes priority", func(t *testing.T) {
		t.Parallel()
		req := BookingRequest{
			Shipment: Shipment{
				ShipmentComment: "Lettera importante",
				Colli: []Colli{
					{Items: []Item{{Description: "should be ignored"}}},
				},
			},
		}
		assert.Equal(t, "Lettera importante", upDocumentContent(req))
	})

	t.Run("falls back to item descriptions", func(t *testing.T) {
		t.Parallel()
		req := BookingRequest{
			Shipment: Shipment{
				Colli: []Colli{
					{Items: []Item{{Description: "Part one"}}},
					{Items: []Item{{Description: "Part two"}}},
				},
			},
		}
		assert.Equal(t, "Part one\nPart two", upDocumentContent(req))
	})

	t.Run("empty item descriptions are skipped", func(t *testing.T) {
		t.Parallel()
		req := BookingRequest{
			Shipment: Shipment{
				Colli: []Colli{
					{Items: []Item{{Description: ""}, {Description: "Only this"}}},
				},
			},
		}
		assert.Equal(t, "Only this", upDocumentContent(req))
	})

	t.Run("no content returns fallback", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Documento", upDocumentContent(BookingRequest{}))
	})
}

// ─── Mock adapter tests ───────────────────────────────────────────────────────

func TestMockUfficioPostaleAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("default returns valid response", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockUfficioPostaleAdapter().BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.NoError(t, err)
		assert.Equal(t, "ufficiopostale", resp.Carrier)
		assert.Equal(t, "confirmed", resp.Status)
		assert.NotEmpty(t, resp.TrackingNumber)
		assert.NotEmpty(t, resp.ShipmentID)
		assert.Contains(t, resp.ShipmentID, "raccomandate/")
		assert.NotEmpty(t, resp.BetaWarning)
	})

	t.Run("override func is called", func(t *testing.T) {
		t.Parallel()
		called := false
		m := &MockUfficioPostaleAdapter{
			BookShipmentFunc: func(_ BookingRequest) (*BookingResponse, error) {
				called = true
				return nil, errors.New("injected error")
			},
		}
		_, err := m.BookShipment(t.Context(), BookingRequest{})
		require.Error(t, err)
		assert.True(t, called)
	})
}

func TestMockUfficioPostaleAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	t.Run("default returns booked status", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockUfficioPostaleAdapter().TrackShipment(t.Context(), "123456789012")
		require.NoError(t, err)
		assert.Equal(t, "ufficiopostale", resp.Carrier)
		assert.Equal(t, "123456789012", resp.TrackingNumber)
		assert.Equal(t, StatusBooked, resp.NormalizedStatus)
		assert.NotEmpty(t, resp.Events)
	})

	t.Run("empty tracking number returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockUfficioPostaleAdapter().TrackShipment(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tracking number must not be empty")
	})

	t.Run("override func is called", func(t *testing.T) {
		t.Parallel()
		called := false
		m := &MockUfficioPostaleAdapter{
			TrackShipmentFunc: func(_ string) (*TrackingResponse, error) {
				called = true
				return nil, errors.New("injected error")
			},
		}
		_, err := m.TrackShipment(t.Context(), "123456789012")
		require.Error(t, err)
		assert.True(t, called)
	})
}

func TestMockUfficioPostaleAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("PDF label", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockUfficioPostaleAdapter().FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abc123",
			Format:         LabelFormatPDF,
		})
		require.NoError(t, err)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.Equal(t, mockLabelData, resp.Data)
		assert.Equal(t, "application/pdf", resp.MimeType)
	})

	t.Run("empty format is accepted", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockUfficioPostaleAdapter().FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abc123",
		})
		require.NoError(t, err)
	})

	t.Run("empty tracking number returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockUfficioPostaleAdapter().FetchLabel(t.Context(), LabelRequest{Format: LabelFormatPDF})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tracking number must not be empty")
	})

	t.Run("unsupported format returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockUfficioPostaleAdapter().FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abc123",
			Format:         LabelFormatPNG,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PDF format is supported")
	})
}

func TestMockUfficioPostaleAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	_, err := NewMockUfficioPostaleAdapter().CancelShipment(t.Context(), "123456789012")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestMockUfficioPostaleAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	_, err := NewMockUfficioPostaleAdapter().UpdateShipment(t.Context(), UpdateRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

// ─── Live adapter unit tests (httptest) ──────────────────────────────────────

func TestUfficioPostaleAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("success raccomandata", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/raccomandate/", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.NotNil(t, body["mittente"])
			assert.NotNil(t, body["destinatari"])
			assert.Equal(t, true, body["opzioni"].(map[string]any)["autoconfirm"])

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[{
				"destinatari":[{
					"id":"abcdef123456",
					"state":"confirmed",
					"NumeroRaccomandata":"123456789012"
				}]
			}]`))
		}))
		defer srv.Close()

		resp, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.NoError(t, err)
		assert.Equal(t, "ufficiopostale", resp.Carrier)
		assert.Equal(t, "123456789012", resp.TrackingNumber)
		assert.Equal(t, "raccomandate/abcdef123456", resp.ShipmentID)
		assert.Equal(t, "confirmed", resp.Status)
		assert.NotEmpty(t, resp.BetaWarning)
	})

	t.Run("ordinaria uses internal ID as tracking number", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/ordinarie/", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[{
				"destinatari":[{"id":"internal-id-001","state":"confirmed","NumeroRaccomandata":""}]
			}]`))
		}))
		defer srv.Close()

		resp, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier: "ufficiopostale",
			Shipment: Shipment{
				Sender:      upTestSender(),
				Receiver:    upTestReceiver(),
				ServiceTier: "ordinaria",
			},
		})
		require.NoError(t, err)
		// Untracked products fall back to internal ID.
		assert.Equal(t, "internal-id-001", resp.TrackingNumber)
		assert.Equal(t, "ordinarie/internal-id-001", resp.ShipmentID)
	})

	t.Run("callback URL is forwarded", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			cb, ok := body["callback"].(map[string]any)
			require.True(t, ok, "callback field missing from payload")
			assert.Equal(t, "https://example.com/webhook", cb["url"])

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[{
				"destinatari":[{"id":"id1","state":"confirmed","NumeroRaccomandata":"TRK001"}]
			}]`))
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:     "ufficiopostale",
			CallbackURL: "https://example.com/webhook",
			Shipment:    Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.NoError(t, err)
	})

	t.Run("unsupported service tier returns error before HTTP call", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier: "ufficiopostale",
			Shipment: Shipment{
				Sender:      upTestSender(),
				Receiver:    upTestReceiver(),
				ServiceTier: "express",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported service tier")
	})

	t.Run("API returns success=false", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"success":false,"message":"authentication failed","data":null}`)
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})

	t.Run("HTTP 4xx returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"invalid token"}`)
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("empty booking items returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[]`))
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no recipients")
	})

	t.Run("empty destinatari returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[{"destinatari":[]}]`))
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "ufficiopostale",
			Shipment: Shipment{Sender: upTestSender(), Receiver: upTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no recipients")
	})
}

func TestUfficioPostaleAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	t.Run("success with events", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/tracking/123456789012", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[
				{"timestamp":1700000000,"descrizione":"Accettato Online","type":"00"},
				{"timestamp":1700086400,"descrizione":"In distribuzione","type":"20"},
				{"timestamp":1700100000,"descrizione":"Consegnato","type":"01"}
			]`))
		}))
		defer srv.Close()

		resp, err := upTestAdapter(srv).TrackShipment(t.Context(), "123456789012")
		require.NoError(t, err)
		assert.Equal(t, "ufficiopostale", resp.Carrier)
		assert.Equal(t, "123456789012", resp.TrackingNumber)
		// Last event wins.
		assert.Equal(t, StatusDelivered, resp.NormalizedStatus)
		assert.Equal(t, "Consegnato", resp.Status)
		assert.Equal(t, "Consegnato", resp.OriginalStatus)
		assert.Len(t, resp.Events, 3)
		assert.Equal(t, StatusBooked, resp.Events[0].NormalizedStatus)
		assert.Equal(t, StatusOutForDelivery, resp.Events[1].NormalizedStatus)
	})

	t.Run("empty event list returns StatusUnknown", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upSuccessEnvelope(`[]`))
		}))
		defer srv.Close()

		resp, err := upTestAdapter(srv).TrackShipment(t.Context(), "123456789012")
		require.NoError(t, err)
		assert.Equal(t, StatusUnknown, resp.NormalizedStatus)
		assert.Empty(t, resp.Events)
	})

	t.Run("empty tracking number returns error before HTTP call", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).TrackShipment(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tracking number must not be empty")
	})

	t.Run("HTTP 4xx returns wrapped error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `not found`)
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).TrackShipment(t.Context(), "999999999999")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "track shipment 999999999999")
	})
}

func TestUfficioPostaleAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("success returns base64 PDF", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/raccomandate/abcdef123456/accettazione", r.URL.Path)
			assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/pdf", r.Header.Get("Accept"))

			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4 stub content"))
		}))
		defer srv.Close()

		resp, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abcdef123456",
			Format:         LabelFormatPDF,
		})
		require.NoError(t, err)
		assert.Equal(t, "ufficiopostale", resp.Carrier)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.Equal(t, "application/pdf", resp.MimeType)
		assert.Equal(t, "raccomandate/abcdef123456", resp.TrackingNumber)
		assert.NotEmpty(t, resp.Data)
	})

	t.Run("empty format is accepted as PDF", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF"))
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abc",
		})
		require.NoError(t, err)
	})

	t.Run("empty tracking number returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{Format: LabelFormatPDF})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tracking number must not be empty")
	})

	t.Run("unsupported format returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/abc",
			Format:         LabelFormatPNG,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PDF format is supported")
	})

	t.Run("shipment ID missing slash returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "justtracking",
			Format:         LabelFormatPDF,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ShipmentID")
	})

	t.Run("HTTP 404 returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "not found")
		}))
		defer srv.Close()

		_, err := upTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "raccomandate/missing-id",
			Format:         LabelFormatPDF,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})
}

func TestUfficioPostaleAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	a := NewUfficioPostaleAdapter("key", false, zap.NewNop())
	_, err := a.CancelShipment(t.Context(), "123456789012")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestUfficioPostaleAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	a := NewUfficioPostaleAdapter("key", false, zap.NewNop())
	_, err := a.UpdateShipment(t.Context(), UpdateRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

// ─── Interface compliance ─────────────────────────────────────────────────────

func TestUfficioPostaleAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ CarrierAdapter = (*UfficioPostaleAdapter)(nil)
	var _ CarrierAdapter = (*MockUfficioPostaleAdapter)(nil)
}
