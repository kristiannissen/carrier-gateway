// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/bring_test.go.
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

func TestMockBringAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("missing TotalWeight", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockBringAdapter{}).BookShipment(BookingRequest{
			Carrier: "bring",
			Shipment: Shipment{
				Sender:   bringTestSender(),
				Receiver: bringTestReceiver(),
				Colli:    []Colli{bringTestColli("colli-1", 5.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")
	})

	t.Run("TotalWeight mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockBringAdapter{}).BookShipment(BookingRequest{
			Carrier: "bring",
			Shipment: Shipment{
				Sender:      bringTestSender(),
				Receiver:    bringTestReceiver(),
				TotalWeight: 3.0,
				Colli:       []Colli{bringTestColli("colli-1", 5.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")
	})

	t.Run("valid request", func(t *testing.T) {
		t.Parallel()
		response, err := (&MockBringAdapter{}).BookShipment(BookingRequest{
			Carrier: "bring",
			Shipment: Shipment{
				Sender:      bringTestSender(),
				Receiver:    bringTestReceiver(),
				TotalWeight: 5.0,
				Colli:       []Colli{bringTestColli("colli-1", 5.0)},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "bring", response.Carrier)
		assert.NotEmpty(t, response.TrackingNumber)
		assert.NotEmpty(t, response.LabelURL)
	})
}

func TestMockBringAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	response, err := (&MockBringAdapter{}).TrackShipment("BR123456789NO")
	require.NoError(t, err)
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "Delivered", response.Status)
	assert.Len(t, response.Events, 3)
}

func TestMockBringAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()

	servicePoints, err := (&MockBringAdapter{}).GetServicePoints(Location{
		City: "Oslo", Country: "NO", PostalCode: "0123",
	})
	require.NoError(t, err)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "BR001", servicePoints[0].ID)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestBringAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	_, err := adapter.BookShipment(BookingRequest{
		Carrier: "bring",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Unisport Group",
				Street:     "Industrivej 10",
				City:       "Copenhagen",
				PostalCode: "2300",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "John Doe",
				Street:     "Storgatan 1",
				City:       "Stockholm",
				PostalCode: "111 22",
				Country:    "SE",
			},
			TotalWeight: 5.0,
			Colli:       []Colli{bringTestColli("box-1", 5.0)},
		},
	})
	require.NoError(t, err)

	payload := *captured

	// customerId must be present at the top level
	assert.Equal(t, adapter.CustomerID, payload["customerId"])

	shipment := bringRequireNested(t, payload, "shipment")

	// "from" — not "sender"
	_, hasSender := shipment["sender"]
	assert.False(t, hasSender, "Bring expects 'from', not 'sender'")
	from := bringRequireNested(t, shipment, "from")
	assert.Equal(t, "Unisport Group", from["name"])
	assert.Equal(t, "Industrivej 10", from["address"])
	assert.Equal(t, "2300", from["postalCode"])
	assert.Equal(t, "Copenhagen", from["city"])
	assert.Equal(t, "DK", from["country"])

	// "to" — not "receiver" or "recipient"
	_, hasReceiver := shipment["receiver"]
	assert.False(t, hasReceiver, "Bring expects 'to', not 'receiver'")
	to := bringRequireNested(t, shipment, "to")
	assert.Equal(t, "John Doe", to["name"])
	assert.Equal(t, "SE", to["country"])

	// parcels — unit-suffixed dimension keys
	parcels := bringRequireArray(t, shipment, "parcels", 1)
	parcel := parcels[0].(map[string]interface{})
	assert.Equal(t, float64(5.0), parcel["weightInKg"])
	assert.Equal(t, float64(10), parcel["lengthInCm"])
	assert.Equal(t, float64(10), parcel["widthInCm"])
	assert.Equal(t, float64(10), parcel["heightInCm"])

	// old flat keys must be absent
	assert.NotContains(t, parcel, "weight", "use 'weightInKg'")
	assert.NotContains(t, parcel, "length", "use 'lengthInCm'")
	assert.NotContains(t, parcel, "width", "use 'widthInCm'")
	assert.NotContains(t, parcel, "height", "use 'heightInCm'")
}

func TestBringAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	req := bringMinimalRequest()
	req.Shipment.TotalWeight = 8.0
	req.Shipment.Colli = []Colli{
		bringTestColli("box-1", 3.0),
		bringTestColli("box-2", 5.0),
	}

	_, err := adapter.BookShipment(req)
	require.NoError(t, err)

	_ = adapter
	shipment := bringRequireNested(t, *captured, "shipment")
	parcels := bringRequireArray(t, shipment, "parcels", 2)

	p0 := parcels[0].(map[string]interface{})
	assert.Equal(t, float64(3.0), p0["weightInKg"])

	p1 := parcels[1].(map[string]interface{})
	assert.Equal(t, float64(5.0), p1["weightInKg"])
}

func TestBringAdapter_BookShipment_ItemsForwarded(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	req := bringMinimalRequest()
	req.Shipment.Colli[0].Items = []Item{
		{Description: "T-Shirt", Weight: 0.5, Quantity: 5, Value: 25.0},
	}

	_, err := adapter.BookShipment(req)
	require.NoError(t, err)

	_ = adapter
	shipment := bringRequireNested(t, *captured, "shipment")
	parcels := bringRequireArray(t, shipment, "parcels", 1)
	parcel := parcels[0].(map[string]interface{})

	items, ok := parcel["items"].([]interface{})
	require.True(t, ok, "items must be present when colli has items")
	require.Len(t, items, 1)

	item := items[0].(map[string]interface{})
	assert.Equal(t, "T-Shirt", item["description"])
	assert.Equal(t, float64(0.5), item["weight"])
	assert.Equal(t, float64(5), item["quantity"])
	assert.Equal(t, float64(25.0), item["value"])
}

func TestBringAdapter_BookShipment_ResponseMapped(t *testing.T) {
	t.Parallel()

	adapter, _ := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	response, err := adapter.BookShipment(bringMinimalRequest())
	require.NoError(t, err)

	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "bring", response.Carrier)
	assert.Equal(t, 125.50, response.Cost)
	assert.Equal(t, "NOK", response.Currency)
	assert.Equal(t, "Standard", response.ServiceLevel)
	assert.Equal(t, "booked", response.Status)
}

func TestBringAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newBringTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(bringMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestBringAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "test-customer", r.Header.Get("X-MyBring-API-Uid"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bringMockTrackingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &BringAdapter{
		APIKey:     "test-key",
		CustomerID: "test-customer",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.TrackShipment("BR123456789NO")
	require.NoError(t, err)

	// Correct endpoint: /tracking/{id}, not /tracking/consignments/{id}
	assert.Equal(t, "/tracking/BR123456789NO", capturedPath)
	assert.Equal(t, "BR123456789NO", resp.TrackingNumber)
	assert.Equal(t, "Delivered", resp.Status)
	assert.Equal(t, "2026-06-05", resp.EstimatedDelivery)
	assert.Len(t, resp.Events, 3)
}

// =========================================================================
// Helpers
// =========================================================================

func newBringTestServer(t *testing.T, statusCode int, body string) (*BringAdapter, *map[string]interface{}) {
	t.Helper()

	var captured map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &captured))
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return &BringAdapter{
		APIKey:     "test-key",
		CustomerID: "test-customer",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}, &captured
}

func bringMockBookingResponse() string {
	return `{"consignmentNumber":"BR123456789NO","labelUrl":"https://mock.bring.com/labels/BR123456789NO.pdf","carrier":"bring","cost":125.50,"currency":"NOK","serviceLevel":"Standard","status":"booked"}`
}

func bringMockTrackingResponse() string {
	return `{"consignmentNumber":"BR123456789NO","carrier":"bring","status":"Delivered","estimatedDelivery":"2026-06-05","events":[{"timestamp":"2026-06-02T14:30:00Z","status":"Picked Up","location":"Copenhagen, DK"},{"timestamp":"2026-06-03T10:00:00Z","status":"In Transit","location":"Malmö, SE"},{"timestamp":"2026-06-04T16:00:00Z","status":"Delivered","location":"Stockholm, SE"}]}`
}

func bringRequireNested(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	v, ok := parent[key].(map[string]interface{})
	require.True(t, ok, "missing nested key %q", key)
	return v
}

func bringRequireArray(t *testing.T, parent map[string]interface{}, key string, wantLen int) []interface{} {
	t.Helper()
	v, ok := parent[key].([]interface{})
	require.True(t, ok, "missing array key %q", key)
	require.Len(t, v, wantLen)
	return v
}

func bringMinimalRequest() BookingRequest {
	return BookingRequest{
		Carrier: "bring",
		Shipment: Shipment{
			Sender:      bringTestSender(),
			Receiver:    bringTestReceiver(),
			TotalWeight: 1.0,
			Colli:       []Colli{bringTestColli("c1", 1.0)},
		},
	}
}

func bringTestSender() Address {
	return Address{
		Name: "Sender", Street: "Street 1",
		City: "Oslo", PostalCode: "0123", Country: "NO",
	}
}

func bringTestReceiver() Address {
	return Address{
		Name: "Receiver", Street: "Street 2",
		City: "Stockholm", PostalCode: "111 22", Country: "SE",
	}
}

func bringTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
	}
}
