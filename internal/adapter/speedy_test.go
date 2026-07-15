// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/speedy_test.go.
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

func speedyTestAdapter(srv *httptest.Server) *SpeedyAdapter {
	a := NewSpeedyAdapter("testuser", "testpass", speedyDefaultServiceID, zap.NewNop())
	a.BaseURL = srv.URL
	a.HTTPClient = srv.Client()
	return a
}

func speedyTestSender() Address {
	return Address{
		Name:        "Test Sender",
		Street:      "Vitosha Blvd",
		HouseNumber: "22",
		City:        "Sofia",
		PostalCode:  "1000",
		Country:     "BG",
		Phone:       "+359888000001",
		Email:       "sender@example.com",
	}
}

func speedyTestReceiver() Address {
	return Address{
		Name:        "Test Receiver",
		Street:      "Tsarigradsko Shose",
		HouseNumber: "135",
		City:        "Sofia",
		PostalCode:  "1784",
		Country:     "BG",
		Phone:       "+359888000002",
		Email:       "receiver@example.com",
	}
}

func speedyTestColli(id string, weight float64) Colli {
	return Colli{ID: id, Weight: weight}
}

// ─── Mock adapter tests ───────────────────────────────────────────────────────

func TestMockSpeedyAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("no colli returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockSpeedyAdapter().BookShipment(t.Context(), BookingRequest{
			Carrier:  "speedy",
			Shipment: Shipment{Sender: speedyTestSender(), Receiver: speedyTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one colli")
	})

	t.Run("success returns tracking number", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockSpeedyAdapter().BookShipment(t.Context(), BookingRequest{
			Carrier: "speedy",
			Shipment: Shipment{
				Sender:   speedyTestSender(),
				Receiver: speedyTestReceiver(),
				Colli:    []Colli{speedyTestColli("c1", 2.0)},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "speedy", resp.Carrier)
		assert.Equal(t, "booked", resp.Status)
		assert.NotEmpty(t, resp.TrackingNumber)
		assert.NotEmpty(t, resp.ShipmentID)
		assert.Len(t, resp.Colli, 1)
	})

	t.Run("override func is called", func(t *testing.T) {
		t.Parallel()
		called := false
		m := &MockSpeedyAdapter{
			BookShipmentFunc: func(r BookingRequest) (*BookingResponse, error) {
				called = true
				return nil, errors.New("injected error")
			},
		}
		_, err := m.BookShipment(t.Context(), BookingRequest{})
		require.Error(t, err)
		assert.True(t, called)
	})
}

func TestMockSpeedyAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().TrackShipment(t.Context(), "SPD0000000001-1")
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "SPD0000000001-1", resp.TrackingNumber)
	assert.NotEmpty(t, resp.Events)
	assert.Equal(t, StatusPickedUp, resp.NormalizedStatus)
}

func TestMockSpeedyAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("PDF label", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockSpeedyAdapter().FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "SPD0000000001-1",
			Format:         LabelFormatPDF,
		})
		require.NoError(t, err)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.Equal(t, mockLabelData, resp.Data)
		assert.Equal(t, "application/pdf", resp.MimeType)
	})

	t.Run("unsupported format returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockSpeedyAdapter().FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "SPD0000000001-1",
			Format:         LabelFormatPNG,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Speedy")
	})
}

func TestMockSpeedyAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().CancelShipment(t.Context(), "SPD0000000001")
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "cancelled", resp.Status)
	assert.Equal(t, "SPD0000000001", resp.TrackingNumber)
}

func TestMockSpeedyAdapter_ManifestAdapter(t *testing.T) {
	t.Parallel()
	m := NewMockSpeedyAdapter()

	_, err := m.UpdatePickup(t.Context(), "X", PickupRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))

	err = m.CancelPickup(t.Context(), "speedy", "X")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = m.CloseManifest(t.Context(), ManifestRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = m.GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestMockSpeedyAdapter_BookPickup(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().BookPickup(t.Context(), PickupRequest{
		Carrier: "speedy",
		Pickup:  PickupWindow{Date: "2026-06-20", CloseTime: "16:00"},
		Contact: PickupContact{Name: "John Doe", Phone: "+359888123456"},
	})
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.NotEmpty(t, resp.ConfirmationNumber)
	assert.Equal(t, "2026-06-20", resp.Date)
}

func TestMockSpeedyAdapter_GetCutoffTime(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().GetCutoffTime(t.Context(), "1000", "BG")
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "13:00", resp.CutoffTime)
}

func TestMockSpeedyAdapter_BookReturn(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().BookReturn(t.Context(), ReturnRequest{
		Carrier:  "speedy",
		Sender:   speedyTestReceiver(),
		Receiver: speedyTestSender(),
	})
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.NotEmpty(t, resp.TrackingNumber)
}

func TestMockSpeedyAdapter_GetReturnShipment(t *testing.T) {
	t.Parallel()

	resp, err := NewMockSpeedyAdapter().GetReturnShipment(t.Context(), "SPD0000000042")
	require.NoError(t, err)
	assert.Equal(t, "SPD0000000042", resp.ID)
	assert.Len(t, resp.Parcels, 1)
}

// ─── Live adapter unit tests (httptest) ──────────────────────────────────────

func TestSpeedyAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/shipment", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)

			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "testuser", body["userName"])

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"id": "SHPID001",
				"parcels": [
					{"id": "PRC001", "seqNo": 1},
					{"id": "PRC002", "seqNo": 2}
				]
			}`)
		}))
		defer srv.Close()

		a := speedyTestAdapter(srv)
		resp, err := a.BookShipment(t.Context(), BookingRequest{
			Carrier: "speedy",
			Shipment: Shipment{
				Sender:   speedyTestSender(),
				Receiver: speedyTestReceiver(),
				Colli: []Colli{
					speedyTestColli("c1", 1.5),
					speedyTestColli("c2", 2.0),
				},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "PRC001", resp.TrackingNumber)
		assert.Equal(t, "SHPID001", resp.ShipmentID)
		assert.Equal(t, "speedy", resp.Carrier)
		assert.Equal(t, "booked", resp.Status)
		assert.Len(t, resp.Colli, 2)
	})

	t.Run("carrier error in response body", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"error":{"context":"service","message":"invalid service ID"}}`)
		}))
		defer srv.Close()

		_, err := speedyTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier: "speedy",
			Shipment: Shipment{
				Sender:   speedyTestSender(),
				Receiver: speedyTestReceiver(),
				Colli:    []Colli{speedyTestColli("c1", 1.0)},
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service ID")
	})

	t.Run("no colli returns error before HTTP call", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("should not reach server")
		}))
		defer srv.Close()

		_, err := speedyTestAdapter(srv).BookShipment(t.Context(), BookingRequest{
			Carrier:  "speedy",
			Shipment: Shipment{Sender: speedyTestSender(), Receiver: speedyTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one colli")
	})
}

func TestSpeedyAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	t.Run("success with operations", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/track", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"parcels": [{
					"parcelId": "PRC001",
					"operations": [
						{"dateTime":"2026-06-15T10:00:00+03:00","operationCode":31,"description":"Out for delivery","operationSiteName":"Sofia Hub"},
						{"dateTime":"2026-06-14T08:00:00+03:00","operationCode":10,"description":"Picked up","operationSiteName":"Sofia Depot"}
					]
				}]
			}`)
		}))
		defer srv.Close()

		resp, err := speedyTestAdapter(srv).TrackShipment(t.Context(), "PRC001")
		require.NoError(t, err)
		assert.Equal(t, "PRC001", resp.TrackingNumber)
		assert.Equal(t, "speedy", resp.Carrier)
		assert.Equal(t, "31", resp.Status)
		assert.Equal(t, StatusOutForDelivery, resp.NormalizedStatus)
		assert.Len(t, resp.Events, 2)
		assert.Equal(t, "Sofia Hub", resp.Events[0].Location)
	})

	t.Run("empty tracking number", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		defer srv.Close()

		_, err := speedyTestAdapter(srv).TrackShipment(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tracking number must not be empty")
	})
}

func TestSpeedyAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/shipment/cancel", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`) // empty JSON = success
		}))
		defer srv.Close()

		resp, err := speedyTestAdapter(srv).CancelShipment(t.Context(), "SHPID001")
		require.NoError(t, err)
		assert.Equal(t, "SHPID001", resp.TrackingNumber)
		assert.Equal(t, "cancelled", resp.Status)
	})

	t.Run("carrier returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"error":{"message":"shipment already picked up"}}`)
		}))
		defer srv.Close()

		_, err := speedyTestAdapter(srv).CancelShipment(t.Context(), "SHPID001")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shipment already picked up")
	})
}

func TestSpeedyAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	a := NewSpeedyAdapter("u", "p", speedyDefaultServiceID, zap.NewNop())
	_, err := a.UpdateShipment(t.Context(), UpdateRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestSpeedyAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("PDF returns base64", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/print", r.URL.Path)
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.0 stub"))
		}))
		defer srv.Close()

		resp, err := speedyTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "PRC001",
			Format:         LabelFormatPDF,
		})
		require.NoError(t, err)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.NotEmpty(t, resp.Data)
		assert.Equal(t, "application/pdf", resp.MimeType)
	})

	t.Run("unsupported format", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		defer srv.Close()

		_, err := speedyTestAdapter(srv).FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "PRC001",
			Format:         LabelFormatPNG,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Speedy")
	})
}

func TestSpeedyAdapter_BookPickup(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pickup", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"orders": [{"id": "PU001", "pickupDate": "2026-06-20"}]
		}`)
	}))
	defer srv.Close()

	resp, err := speedyTestAdapter(srv).BookPickup(t.Context(), PickupRequest{
		Carrier: "speedy",
		Pickup:  PickupWindow{Date: "2026-06-20", ReadyTime: "09:00", CloseTime: "17:00"},
		Contact: PickupContact{Name: "John", Phone: "+359888000001"},
	})
	require.NoError(t, err)
	assert.Equal(t, "PU001", resp.ConfirmationNumber)
	assert.Equal(t, "2026-06-20", resp.Date)
	assert.Equal(t, "booked", resp.Status)
}

func TestSpeedyAdapter_ManifestNotSupported(t *testing.T) {
	t.Parallel()
	a := NewSpeedyAdapter("u", "p", speedyDefaultServiceID, zap.NewNop())

	_, err := a.UpdatePickup(t.Context(), "X", PickupRequest{})
	assert.True(t, errors.Is(err, ErrNotSupported))

	err = a.CancelPickup(t.Context(), "speedy", "X")
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = a.CloseManifest(t.Context(), ManifestRequest{})
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = a.GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = a.GetPickupByID(t.Context(), "X")
	assert.True(t, errors.Is(err, ErrNotSupported))

	_, err = a.ListPickups(t.Context(), ListPickupsRequest{})
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestSpeedyAdapter_GetCutoffTime(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pickup/terms", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cutoffs":["2026-06-20T13:00:00+03:00","2026-06-21T13:00:00+03:00"]}`)
	}))
	defer srv.Close()

	resp, err := speedyTestAdapter(srv).GetCutoffTime(t.Context(), "1000", "BG")
	require.NoError(t, err)
	assert.Equal(t, "speedy", resp.Carrier)
	assert.Equal(t, "13:00", resp.CutoffTime)
}

func TestSpeedyAdapter_GetReturnShipment(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/shipment/SHPID001/secondary", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"shipments": [
				{"id": "RET001", "type": "RETURN_SHIPMENT", "parcels": [{"id": "RETPRC001"}]}
			]
		}`)
	}))
	defer srv.Close()

	resp, err := speedyTestAdapter(srv).GetReturnShipment(t.Context(), "SHPID001")
	require.NoError(t, err)
	assert.Equal(t, "SHPID001", resp.ID)
	assert.Len(t, resp.Parcels, 1)
	assert.Equal(t, "RETPRC001", resp.Parcels[0].TrackingNumber)
}

// ─── Interface compliance ─────────────────────────────────────────────────────

func TestSpeedyAdapter_ImplementsInterfaces(t *testing.T) {
	t.Parallel()

	var _ CarrierAdapter = (*SpeedyAdapter)(nil)
	var _ ManifestAdapter = (*SpeedyAdapter)(nil)
	var _ PickupQuerier = (*SpeedyAdapter)(nil)
	var _ ReturnAdapter = (*SpeedyAdapter)(nil)
	var _ ReturnQuerier = (*SpeedyAdapter)(nil)

	var _ CarrierAdapter = (*MockSpeedyAdapter)(nil)
	var _ ManifestAdapter = (*MockSpeedyAdapter)(nil)
	var _ PickupQuerier = (*MockSpeedyAdapter)(nil)
	var _ ReturnAdapter = (*MockSpeedyAdapter)(nil)
	var _ ReturnQuerier = (*MockSpeedyAdapter)(nil)
}
