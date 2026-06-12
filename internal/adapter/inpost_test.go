// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/inpost_test.go.
package adapter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// Mock adapter tests
// =========================================================================

func TestMockInPostAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("missing TotalWeight", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockInPostAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "inpost",
			Shipment: Shipment{
				Sender:   inpostTestSender(),
				Receiver: inpostTestReceiver(),
				Colli:    []Colli{inpostTestColli("c1", 2.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")
	})

	t.Run("TotalWeight mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockInPostAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "inpost",
			Shipment: Shipment{
				Sender:      inpostTestSender(),
				Receiver:    inpostTestReceiver(),
				TotalWeight: 1.0,
				Colli:       []Colli{inpostTestColli("c1", 2.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")
	})

	t.Run("valid request", func(t *testing.T) {
		t.Parallel()
		response, err := (&MockInPostAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "inpost",
			Shipment: Shipment{
				Sender:      inpostTestSender(),
				Receiver:    inpostTestReceiver(),
				TotalWeight: 2.0,
				Colli:       []Colli{inpostTestColli("c1", 2.0)},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "inpost", response.Carrier)
		assert.NotEmpty(t, response.TrackingNumber)
		assert.NotEmpty(t, response.ShipmentID)
		assert.Equal(t, "PLN", response.Currency)
		assert.Equal(t, "WAR001", response.LockerId)
	})
}

func TestMockInPostAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	response, err := (&MockInPostAdapter{}).TrackShipment(t.Context(), "INPOST123456789PL")
	require.NoError(t, err)
	assert.Equal(t, "INPOST123456789PL", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Equal(t, "inpost", response.Carrier)
	assert.Len(t, response.Events, 2)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestInPostAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), BookingRequest{
		Carrier: "inpost",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Shop",
				Street:     "Sender Street",
				City:       "Warsaw",
				PostalCode: "00-001",
				Country:    "PL",
				Phone:      "+4812345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "John Kowalski",
				Street:     "Receiver Avenue",
				City:       "Krakow",
				PostalCode: "30-001",
				Country:    "PL",
				Phone:      "+48987654321",
				Email:      "john.kowalski@example.com",
			},
			TotalWeight: 2.0,
			Colli:       []Colli{inpostTestColli("box-1", 2.0)},
		},
	})
	require.NoError(t, err)

	payload := *captured
	shipment := inpostRequireNested(t, payload, "shipment")

	// Sender
	sender := inpostRequireNested(t, shipment, "sender")
	assert.Equal(t, "Sender Shop", sender["name"])
	senderAddr := inpostRequireNested(t, sender, "address")
	assert.Equal(t, "Sender Street", senderAddr["streetName"]) // Street → streetName
	assert.Equal(t, "Warsaw", senderAddr["city"])
	assert.Equal(t, "00-001", senderAddr["postalCode"])
	assert.Equal(t, "PL", senderAddr["country"])
	senderContact := inpostRequireNested(t, sender, "contact")
	assert.Equal(t, "+4812345678", senderContact["phone"])
	assert.Equal(t, "sender@example.com", senderContact["email"])

	// Recipient — must not be "receiver"
	_, hasReceiver := shipment["receiver"]
	assert.False(t, hasReceiver, "InPost expects 'recipient', not 'receiver'")
	recipient := inpostRequireNested(t, shipment, "recipient")
	assert.Equal(t, "John Kowalski", recipient["name"])
	recipientAddr := inpostRequireNested(t, recipient, "address")
	assert.Equal(t, "Receiver Avenue", recipientAddr["streetName"])
	assert.Equal(t, "PL", recipientAddr["country"])
	recipientContact := inpostRequireNested(t, recipient, "contact")
	assert.Equal(t, "+48987654321", recipientContact["phone"])

	// Service block
	service := inpostRequireNested(t, shipment, "service")
	assert.Equal(t, "INPOST_STANDARD", service["id"])

	// Parcels
	parcels := inpostRequireArray(t, shipment, "parcels", 1)
	parcel := parcels[0].(map[string]any)
	assert.Equal(t, "1", parcel["id"])
	assert.Equal(t, float64(2.0), parcel["weight"]) // kg, no conversion
	dims := inpostRequireNested(t, parcel, "dimensions")
	assert.Equal(t, float64(10), dims["length"])
	assert.Equal(t, float64(10), dims["width"])
	assert.Equal(t, float64(10), dims["height"])
}

func TestInPostAdapter_BookShipment_NoLockerByDefault(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	require.NoError(t, err)

	shipment := inpostRequireNested(t, *captured, "shipment")
	service := inpostRequireNested(t, shipment, "service")
	assert.NotContains(t, service, "targetLocker",
		"targetLocker must be absent until a dedicated LockerID field is introduced")
}

func TestInPostAdapter_BookShipment_ReferenceForwarded(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.IdempotencyKey = "INPOST-ORDER-12345"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := inpostRequireNested(t, *captured, "shipment")
	assert.Equal(t, "INPOST-ORDER-12345", shipment["reference"])
}

func TestInPostAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.Shipment.TotalWeight = 5.0
	req.Shipment.Colli = []Colli{
		inpostTestColli("box-1", 2.0),
		inpostTestColli("box-2", 3.0),
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := inpostRequireNested(t, *captured, "shipment")
	parcels := inpostRequireArray(t, shipment, "parcels", 2)

	p0 := parcels[0].(map[string]any)
	assert.Equal(t, "1", p0["id"])
	assert.Equal(t, float64(2.0), p0["weight"])

	p1 := parcels[1].(map[string]any)
	assert.Equal(t, "2", p1["id"])
	assert.Equal(t, float64(3.0), p1["weight"])
}

func TestInPostAdapter_BookShipment_ResponseMapped(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	response, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	require.NoError(t, err)

	assert.Equal(t, "INPOST-550e8400-e29b-41d4-a716-446655440007", response.ShipmentID)
	assert.Equal(t, "INPOST123456789PL", response.TrackingNumber)
	assert.Equal(t, "inpost", response.Carrier)
	assert.Equal(t, 8.00, response.Cost)
	assert.Equal(t, "PLN", response.Currency)
	assert.Equal(t, "booked", response.Status)
	assert.Equal(t, "WAR001", response.LockerId)
}

func TestInPostAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestInPostAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"trackingNumber":"INPOST123456789PL","status":"In Transit","events":[{"timestamp":"2026-06-01T10:00:00Z","status":"Picked Up","location":"Warsaw, PL"}]}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &InPostAdapter{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.TrackShipment(t.Context(), "INPOST123456789PL")
	require.NoError(t, err)

	assert.Equal(t, "/tracking/INPOST123456789PL", capturedPath)
	assert.Equal(t, "INPOST123456789PL", resp.TrackingNumber)
	assert.Equal(t, "In Transit", resp.Status)
	assert.Equal(t, "inpost", resp.Carrier)
	assert.Len(t, resp.Events, 1)
}

func TestInPostAdapter_BookShipment_ServicePoint(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.Shipment.Receiver = Address{
		Name:           "Jan Kowalski",
		Country:        "PL",
		Phone:          "+48987654321",
		ServicePointID: "WAR001",
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := inpostRequireNested(t, *captured, "shipment")
	service := inpostRequireNested(t, shipment, "service")
	assert.Equal(t, "WAR001", service["targetLocker"])
}

// =========================================================================
// Helpers
// =========================================================================

func newInPostTestServer(t *testing.T, statusCode int, body string) (*InPostAdapter, *map[string]any) {
	t.Helper()

	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &captured))
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return &InPostAdapter{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}, &captured
}

func inpostMockBookingResponse() string {
	return `{"shipmentId":"INPOST-550e8400-e29b-41d4-a716-446655440007","trackingNumber":"INPOST123456789PL","labelUrl":"https://mock.inpost.pl/labels/550e8400.pdf","status":"booked","cost":8.00,"currency":"PLN","lockerId":"WAR001"}`
}

func inpostRequireNested(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	require.True(t, ok, "missing nested key %q", key)
	return v
}

func inpostRequireArray(t *testing.T, parent map[string]any, key string, wantLen int) []any {
	t.Helper()
	v, ok := parent[key].([]any)
	require.True(t, ok, "missing array key %q", key)
	require.Len(t, v, wantLen)
	return v
}

func inpostMinimalRequest() BookingRequest {
	return BookingRequest{
		Carrier: "inpost",
		Shipment: Shipment{
			Sender:      inpostTestSender(),
			Receiver:    inpostTestReceiver(),
			TotalWeight: 2.0,
			Colli:       []Colli{inpostTestColli("c1", 2.0)},
		},
	}
}

func inpostTestSender() Address {
	return Address{
		Name: "Sender Shop", Street: "Sender Street",
		City: "Warsaw", PostalCode: "00-001", Country: "PL",
	}
}

func inpostTestReceiver() Address {
	return Address{
		Name: "John Kowalski", Street: "Receiver Avenue",
		City: "Krakow", PostalCode: "30-001", Country: "PL",
	}
}

func inpostTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
	}
}
