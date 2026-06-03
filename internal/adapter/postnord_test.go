// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/postnord_test.go.
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

func TestMockPostNordAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("missing TotalWeight", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockPostNordAdapter{}).BookShipment(BookingRequest{
			Carrier: "postnord",
			Shipment: Shipment{
				Sender:   postnordTestSender(),
				Receiver: postnordTestReceiver(),
				Colli:    []Colli{postnordTestColli("colli-1", 10.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")
	})

	t.Run("TotalWeight mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockPostNordAdapter{}).BookShipment(BookingRequest{
			Carrier: "postnord",
			Shipment: Shipment{
				Sender:      postnordTestSender(),
				Receiver:    postnordTestReceiver(),
				TotalWeight: 5.0,
				Colli:       []Colli{postnordTestColli("colli-1", 10.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")
	})

	t.Run("valid request", func(t *testing.T) {
		t.Parallel()
		response, err := (&MockPostNordAdapter{}).BookShipment(BookingRequest{
			Carrier: "postnord",
			Shipment: Shipment{
				Sender:      postnordTestSender(),
				Receiver:    postnordTestReceiver(),
				TotalWeight: 10.0,
				Colli:       []Colli{postnordTestColli("colli-1", 10.0)},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "postnord", response.Carrier)
		assert.NotEmpty(t, response.TrackingNumber)
		assert.NotEmpty(t, response.LabelURL)
	})
}

func TestMockPostNordAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	response, err := (&MockPostNordAdapter{}).TrackShipment("PN123456789DK")
	require.NoError(t, err)
	assert.Equal(t, "PN123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 2)
}

func TestMockPostNordAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()

	servicePoints, err := (&MockPostNordAdapter{}).GetServicePoints(Location{
		City: "Copenhagen", Country: "DK", PostalCode: "12345",
	})
	require.NoError(t, err)
	assert.Len(t, servicePoints, 2)
	assert.Equal(t, "sp_123", servicePoints[0].ID)
	assert.Equal(t, "PostNord Copenhagen", servicePoints[0].Name)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestPostNordAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusCreated,
		`{"trackingNumber":"PN123456789DK","labelUrl":"https://postnord.com/label.pdf","carrier":"postnord"}`)

	_, err := adapter.BookShipment(BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Unisport Group",
				Street:     "Industrivej 10",
				City:       "Copenhagen",
				PostalCode: "2300",
				Country:    "DK",
				Phone:      "+4512345678",
				Email:      "logistics@unisport.dk",
			},
			Receiver: Address{
				Name:       "John Doe",
				Street:     "Storgatan 1",
				City:       "Stockholm",
				PostalCode: "111 22",
				Country:    "SE",
				Phone:      "+46123456789",
				Email:      "john@example.com",
			},
			TotalWeight: 1.5,
			Colli:       []Colli{postnordTestColli("box-1", 1.5)},
		},
	})
	require.NoError(t, err)

	shipment := requireShipment(t, *captured)

	// Sender
	sender := requireParty(t, shipment, "sender")
	assert.Equal(t, "Unisport Group", sender["name"])
	senderAddr := requireNested(t, sender, "address")
	assert.Equal(t, "Industrivej 10", senderAddr["street"])
	assert.Equal(t, "Copenhagen", senderAddr["city"])
	assert.Equal(t, "2300", senderAddr["postalCode"])
	assert.Equal(t, "DK", senderAddr["country"])
	senderContact := requireNested(t, sender, "contact")
	assert.Equal(t, "+4512345678", senderContact["phone"])
	assert.Equal(t, "logistics@unisport.dk", senderContact["email"])

	// Recipient — must be "recipient", not "receiver"
	_, hasReceiver := shipment["receiver"]
	assert.False(t, hasReceiver, "PostNord expects 'recipient', not 'receiver'")
	recipient := requireParty(t, shipment, "recipient")
	assert.Equal(t, "John Doe", recipient["name"])
	recipientAddr := requireNested(t, recipient, "address")
	assert.Equal(t, "Storgatan 1", recipientAddr["street"])
	recipientContact := requireNested(t, recipient, "contact")
	assert.Equal(t, "+46123456789", recipientContact["phone"])
	assert.Equal(t, "john@example.com", recipientContact["email"])

	// Service block
	service := requireNested(t, shipment, "service")
	assert.Equal(t, "1700", service["id"])
	assert.Equal(t, "Parcels", service["product"])

	// Parcels
	parcels := requireParcels(t, shipment, 1)
	parcel := parcels[0].(map[string]interface{})
	assert.Equal(t, "1", parcel["id"])               // sequential, not colli.ID
	assert.Equal(t, float64(1500), parcel["weight"]) // kg → grams
	assert.NotContains(t, parcel, "reference")
	dims := requireNested(t, parcel, "dimensions")
	assert.Equal(t, float64(10), dims["length"])
	assert.Equal(t, float64(10), dims["width"])
	assert.Equal(t, float64(10), dims["height"])
}

func TestPostNordAdapter_BookShipment_IdempotencyKey(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusCreated,
		`{"trackingNumber":"PN999","labelUrl":"https://postnord.com/label.pdf","carrier":"postnord"}`)

	req := postnordMinimalRequest()
	req.IdempotencyKey = "unique-key-123"

	_, err := adapter.BookShipment(req)
	require.NoError(t, err)

	shipment := requireShipment(t, *captured)
	assert.Equal(t, "unique-key-123", shipment["idempotencyKey"])
}

func TestPostNordAdapter_BookShipment_OptionalFields(t *testing.T) {
	t.Parallel()

	mockResp := `{"trackingNumber":"PN999","labelUrl":"https://postnord.com/label.pdf","carrier":"postnord"}`

	t.Run("omitted when empty", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusCreated, mockResp)

		_, err := adapter.BookShipment(postnordMinimalRequest())
		require.NoError(t, err)

		shipment := requireShipment(t, *captured)
		assert.NotContains(t, shipment, "incoterms")
		assert.NotContains(t, shipment, "hsCode")
		assert.NotContains(t, shipment, "idempotencyKey")
	})

	t.Run("included when set", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusCreated, mockResp)

		req := postnordMinimalRequest()
		req.Shipment.Incoterms = "DDP"
		req.Shipment.HSCode = "6104.43"

		_, err := adapter.BookShipment(req)
		require.NoError(t, err)

		shipment := requireShipment(t, *captured)
		assert.Equal(t, "DDP", shipment["incoterms"])
		assert.Equal(t, "6104.43", shipment["hsCode"])
	})
}

func TestPostNordAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusCreated,
		`{"trackingNumber":"PN999","labelUrl":"https://postnord.com/label.pdf","carrier":"postnord"}`)

	req := postnordMinimalRequest()
	req.Shipment.TotalWeight = 4.5
	req.Shipment.Colli = []Colli{
		postnordTestColli("box-1", 1.5),
		postnordTestColli("box-2", 3.0),
	}

	_, err := adapter.BookShipment(req)
	require.NoError(t, err)

	shipment := requireShipment(t, *captured)
	parcels := requireParcels(t, shipment, 2)

	p0 := parcels[0].(map[string]interface{})
	assert.Equal(t, "1", p0["id"])
	assert.Equal(t, float64(1500), p0["weight"])

	p1 := parcels[1].(map[string]interface{})
	assert.Equal(t, "2", p1["id"])
	assert.Equal(t, float64(3000), p1["weight"])
}

func TestPostNordAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newPostNordTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(postnordMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// =========================================================================
// Helpers
// =========================================================================

// newPostNordTestServer starts an httptest.Server that captures the request
// body and responds with statusCode and body. The server is closed
// automatically when the test ends.
func newPostNordTestServer(t *testing.T, statusCode int, body string) (*PostNordAdapter, *map[string]interface{}) {
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

	return &PostNordAdapter{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}, &captured
}

// requireShipment extracts and returns the top-level "shipment" object from a
// captured PostNord request payload, failing the test if it is absent.
func requireShipment(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	shipment, ok := payload["shipment"].(map[string]interface{})
	require.True(t, ok, "payload missing top-level 'shipment' key")
	return shipment
}

// requireParty extracts a sender/recipient object by key, failing the test if absent.
func requireParty(t *testing.T, shipment map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	party, ok := shipment[key].(map[string]interface{})
	require.True(t, ok, "shipment missing '%s' key", key)
	return party
}

// requireNested extracts a nested map by key, failing the test if absent.
func requireNested(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	nested, ok := parent[key].(map[string]interface{})
	require.True(t, ok, "object missing nested '%s' key", key)
	return nested
}

// requireParcels extracts the parcels slice and asserts the expected length.
func requireParcels(t *testing.T, shipment map[string]interface{}, wantLen int) []interface{} {
	t.Helper()
	parcels, ok := shipment["parcels"].([]interface{})
	require.True(t, ok, "shipment missing 'parcels' array")
	require.Len(t, parcels, wantLen)
	return parcels
}

func postnordMinimalRequest() BookingRequest {
	return BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender:      postnordTestSender(),
			Receiver:    postnordTestReceiver(),
			TotalWeight: 1.0,
			Colli:       []Colli{postnordTestColli("c1", 1.0)},
		},
	}
}

func postnordTestSender() Address {
	return Address{
		Name: "Sender", Street: "Street 1",
		City: "Copenhagen", PostalCode: "2300", Country: "DK",
	}
}

func postnordTestReceiver() Address {
	return Address{
		Name: "Receiver", Street: "Street 2",
		City: "Stockholm", PostalCode: "111 22", Country: "SE",
	}
}

func postnordTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
	}
}
