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
		_, err := (&MockPostNordAdapter{}).BookShipment(t.Context(), BookingRequest{
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
		_, err := (&MockPostNordAdapter{}).BookShipment(t.Context(), BookingRequest{
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
		response, err := (&MockPostNordAdapter{}).BookShipment(t.Context(), BookingRequest{
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

	response, err := (&MockPostNordAdapter{}).TrackShipment(t.Context(), "PN123456789DK")
	require.NoError(t, err)
	assert.Equal(t, "PN123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 2)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestPostNordAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	srv, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	_, err := srv.BookShipment(t.Context(), BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:        "Unisport Group",
				Street:      "Industrivej",
				HouseNumber: "10",
				City:        "Copenhagen",
				PostalCode:  "2300",
				Country:     "DK",
				Phone:       "+4512345678",
				Email:       "logistics@unisport.dk",
			},
			Receiver: Address{
				Name:        "John Doe",
				Street:      "Storgatan",
				HouseNumber: "1",
				City:        "Stockholm",
				PostalCode:  "111 22",
				Country:     "SE",
				Phone:       "+46123456789",
				Email:       "john@example.com",
			},
			TotalWeight: 1.5,
			Colli:       []Colli{postnordTestColli("box-1", 1.5)},
		},
	})
	require.NoError(t, err)

	shipment := requirePostNordShipment(t, *captured)

	// Sender
	sender := requireNested(t, shipment, "sender")
	assert.Equal(t, "Unisport Group", sender["name"])
	senderAddr := requireNested(t, sender, "address")
	assert.Equal(t, "Industrivej 10", senderAddr["street"])
	assert.Equal(t, "Copenhagen", senderAddr["city"])
	assert.Equal(t, "2300", senderAddr["postalCode"])
	assert.Equal(t, "DK", senderAddr["countryCode"])

	// Receiver
	receiver := requireNested(t, shipment, "receiver")
	assert.Equal(t, "John Doe", receiver["name"])
	receiverAddr := requireNested(t, receiver, "address")
	assert.Equal(t, "Storgatan 1", receiverAddr["street"])
	assert.Equal(t, "SE", receiverAddr["countryCode"])

	// Service — home delivery default
	service := requireNested(t, shipment, "service")
	assert.Equal(t, "4200", service["serviceId"])

	// Parcels — weight in kg not grams
	parcels := requirePostNordParcels(t, shipment, 1)
	parcel := parcels[0].(map[string]interface{})
	assert.Equal(t, 1.5, parcel["weight"])
	assert.NotContains(t, parcel, "id") // PostNord v1 does not use a parcel id field
}

func TestPostNordAdapter_BookShipment_ServicePoint(t *testing.T) {
	t.Parallel()

	srv, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.Shipment.Receiver = Address{
		Name:           "Anna Svensson",
		Country:        "SE",
		Phone:          "+46701234567",
		ServicePointID: "95763",
	}

	resp, err := srv.BookShipment(t.Context(), req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.TrackingNumber)

	shipment := requirePostNordShipment(t, *captured)

	// Service ID must be 2100 for service point delivery
	service := requireNested(t, shipment, "service")
	assert.Equal(t, "2100", service["serviceId"])

	// servicePoint block must be present
	servicePoint := requireNested(t, shipment, "servicePoint")
	assert.Equal(t, "95763", servicePoint["servicePointId"])
}

func TestPostNordAdapter_BookShipment_IdempotencyKey(t *testing.T) {
	t.Parallel()

	srv, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.IdempotencyKey = "order-98765"

	_, err := srv.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipment := requirePostNordShipment(t, *captured)
	assert.Equal(t, "order-98765", shipment["shipmentReference"])
}

func TestPostNordAdapter_BookShipment_APIKeyInQueryParam(t *testing.T) {
	t.Parallel()

	var capturedURL string
	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(postnordMockBookingResponse()))
	}))
	defer testSrv.Close()

	adapter := &PostNordAdapter{
		APIKey:     "test-api-key",
		BaseURL:    testSrv.URL,
		HTTPClient: testSrv.Client(),
	}

	_, err := adapter.BookShipment(t.Context(), postnordMinimalRequest())
	require.NoError(t, err)

	// API key must appear as a query parameter, not a header
	assert.Contains(t, capturedURL, "apikey=test-api-key")
}

func TestPostNordAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	srv, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.Shipment.TotalWeight = 4.5
	req.Shipment.Colli = []Colli{
		postnordTestColli("box-1", 1.5),
		postnordTestColli("box-2", 3.0),
	}

	_, err := srv.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipment := requirePostNordShipment(t, *captured)
	requirePostNordParcels(t, shipment, 2)
}

func TestPostNordAdapter_BookShipment_HouseNumberConcatenated(t *testing.T) {
	t.Parallel()

	srv, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.Shipment.Sender.Street = "Industrivej"
	req.Shipment.Sender.HouseNumber = "42"

	_, err := srv.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipment := requirePostNordShipment(t, *captured)
	sender := requireNested(t, shipment, "sender")
	addr := requireNested(t, sender, "address")
	assert.Equal(t, "Industrivej 42", addr["street"])
}

func TestPostNordAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	srv, _ := newPostNordTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := srv.BookShipment(t.Context(), postnordMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestPostNordAdapter_SupportsNativeIdempotency(t *testing.T) {
	t.Parallel()
	assert.True(t, SupportsNativeIdempotency("postnord"))
}

// =========================================================================
// Helpers
// =========================================================================

// postnordMockBookingResponse returns a minimal valid PostNord booking response.
func postnordMockBookingResponse() string {
	return `{
		"shipmentResponse": {
			"shipments": [
				{
					"status": "CREATED",
					"shipmentIdentification": "DK123456789SE",
					"parcels": [
						{"parcelIdentification": "00370730254433219997", "sequenceNumber": 1}
					],
					"labels": [
						{"labelType": "PDF", "resolution": "203", "content": "JVBERi0xLjQ="}
					]
				}
			]
		}
	}`
}

// newPostNordTestServer starts an httptest.Server that captures the request
// body and responds with statusCode and body.
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

// requireNested extracts a nested map by key, failing the test if absent.
func requireNested(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	nested, ok := parent[key].(map[string]interface{})
	require.True(t, ok, "object missing nested '%s' key", key)
	return nested
}

// requirePostNordShipment extracts the first shipment from the "shipments" array.
func requirePostNordShipment(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	shipments, ok := payload["shipments"].([]interface{})
	require.True(t, ok, "payload missing top-level 'shipments' array")
	require.NotEmpty(t, shipments, "shipments array must not be empty")
	shipment, ok := shipments[0].(map[string]interface{})
	require.True(t, ok, "shipments[0] is not an object")
	return shipment
}

// requirePostNordParcels extracts the parcels array and asserts the expected length.
func requirePostNordParcels(t *testing.T, shipment map[string]interface{}, wantLen int) []interface{} {
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
		Name:        "Unisport Group",
		Street:      "Industrivej",
		HouseNumber: "10",
		City:        "Copenhagen",
		PostalCode:  "2300",
		Country:     "DK",
		Phone:       "+4512345678",
		Email:       "logistics@unisport.dk",
	}
}

func postnordTestReceiver() Address {
	return Address{
		Name:        "Test Receiver",
		Street:      "Storgatan",
		HouseNumber: "1",
		City:        "Stockholm",
		PostalCode:  "111 22",
		Country:     "SE",
		Phone:       "+46701234567",
		Email:       "receiver@example.com",
	}
}

func postnordTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 20, Width: 15, Height: 10},
		Items:      []Item{{Description: "Sports goods", Weight: weightKg, Quantity: 1}},
	}
}
