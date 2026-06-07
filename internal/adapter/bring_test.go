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
		_, err := (&MockBringAdapter{}).BookShipment(t.Context(), BookingRequest{
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
		_, err := (&MockBringAdapter{}).BookShipment(t.Context(), BookingRequest{
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
		response, err := (&MockBringAdapter{}).BookShipment(t.Context(), BookingRequest{
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

	response, err := (&MockBringAdapter{}).TrackShipment(t.Context(), "BR123456789NO")
	require.NoError(t, err)
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "Delivered", response.Status)
	assert.Len(t, response.Events, 3)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestBringAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), BookingRequest{
		Carrier: "bring",
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
			TotalWeight: 5.0,
			Colli:       []Colli{bringTestColli("box-1", 5.0)},
		},
	})
	require.NoError(t, err)

	payload := *captured

	// Top-level must be "consignments" array
	_, hasCustomerID := payload["customerId"]
	assert.False(t, hasCustomerID, "Bring v2 does not use top-level customerId")
	consignments := bringRequireArray(t, payload, "consignments", 1)
	consignment := consignments[0].(map[string]interface{})

	assert.NotEmpty(t, consignment["shippingDateTime"])

	// parties.sender
	parties := bringRequireNested(t, consignment, "parties")
	sender := bringRequireNested(t, parties, "sender")
	assert.Equal(t, "Unisport Group", sender["name"])
	assert.Equal(t, "Industrivej 10", sender["addressLine"])
	assert.Equal(t, "2300", sender["postalCode"])
	assert.Equal(t, "Copenhagen", sender["city"])
	assert.Equal(t, "DK", sender["countryCode"])

	// parties.recipient — not "to" or "receiver"
	_, hasTo := parties["to"]
	assert.False(t, hasTo, "Bring expects 'recipient', not 'to'")
	recipient := bringRequireNested(t, parties, "recipient")
	assert.Equal(t, "John Doe", recipient["name"])
	assert.Equal(t, "Storgatan 1", recipient["addressLine"])
	assert.Equal(t, "SE", recipient["countryCode"])

	// product.id — home delivery default
	product := bringRequireNested(t, consignment, "product")
	assert.Equal(t, "HOME_DELIVERY_PARCEL", product["id"])

	// packages — dimensions nested
	packages := bringRequireArray(t, consignment, "packages", 1)
	pkg := packages[0].(map[string]interface{})
	assert.Equal(t, float64(5.0), pkg["weightInKg"])
	assert.NotEmpty(t, pkg["goodsDescription"])
	dims := bringRequireNested(t, pkg, "dimensions")
	assert.Equal(t, float64(10), dims["lengthInCm"])
	assert.Equal(t, float64(10), dims["widthInCm"])
	assert.Equal(t, float64(10), dims["heightInCm"])

	// old flat keys must be absent
	assert.NotContains(t, pkg, "lengthInCm", "dimensions must be nested")
	assert.NotContains(t, pkg, "widthInCm", "dimensions must be nested")
	assert.NotContains(t, pkg, "heightInCm", "dimensions must be nested")

	_ = adapter
}

func TestBringAdapter_BookShipment_ProductCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		deliveryType    string
		hasServicePoint bool
		wantProductID   string
	}{
		{"default home delivery", "", false, "HOME_DELIVERY_PARCEL"},
		{"explicit home", "home", false, "HOME_DELIVERY_PARCEL"},
		{"business delivery", "business", false, "BUSINESS_PARCEL"},
		{"service point via DeliveryType", "servicepoint", false, "PICKUP_PARCEL"},
		{"service point via ServicePointID", "", true, "PICKUP_PARCEL"},
		{"return", "return", false, "PICKUP_PARCEL"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := bringProductID(tc.deliveryType, tc.hasServicePoint)
			assert.Equal(t, tc.wantProductID, result)
		})
	}
}

func TestBringAdapter_BookShipment_AuthHeaders(t *testing.T) {
	t.Parallel()

	var capturedUID, capturedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID = r.Header.Get("X-MyBring-API-Uid")
		capturedKey = r.Header.Get("X-MyBring-API-Key")
		// Bearer must NOT be present
		assert.Empty(t, r.Header.Get("Authorization"), "Bring does not use Authorization header")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bringMockBookingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &BringAdapter{
		APIKey:     "test-api-key",
		CustomerID: "test@example.com",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	_, err := adapter.BookShipment(t.Context(), bringMinimalRequest())
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", capturedUID)
	assert.Equal(t, "test-api-key", capturedKey)
}

func TestBringAdapter_BookShipment_Endpoint(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bringMockBookingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &BringAdapter{
		APIKey: "k", CustomerID: "u",
		BaseURL: srv.URL, HTTPClient: srv.Client(),
	}
	_, err := adapter.BookShipment(t.Context(), bringMinimalRequest())
	require.NoError(t, err)
	assert.Equal(t, "/booking/api/shipment", capturedPath)
}

func TestBringAdapter_BookShipment_ServicePoint(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	req := bringMinimalRequest()
	req.Shipment.Receiver = Address{
		Name:           "Recipient Name",
		Street:         "Storgatan",
		HouseNumber:    "1",
		City:           "Stockholm",
		PostalCode:     "111 22",
		Country:        "SE",
		Phone:          "+46701234567",
		ServicePointID: "pp_456",
	}

	resp, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "pp_456", resp.ServicePointID)

	consignments := bringRequireArray(t, *captured, "consignments", 1)
	consignment := consignments[0].(map[string]interface{})
	parties := bringRequireNested(t, consignment, "parties")
	recipient := bringRequireNested(t, parties, "recipient")

	assert.Equal(t, "pp_456", recipient["pickupPointId"])

	product := bringRequireNested(t, consignment, "product")
	assert.Equal(t, "PICKUP_PARCEL", product["id"])

	_ = adapter
}

func TestBringAdapter_BookShipment_IdempotencyKey(t *testing.T) {
	t.Parallel()

	adapter, captured := newBringTestServer(t, http.StatusOK, bringMockBookingResponse())

	req := bringMinimalRequest()
	req.IdempotencyKey = "order-98765"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	consignments := bringRequireArray(t, *captured, "consignments", 1)
	consignment := consignments[0].(map[string]interface{})
	assert.Equal(t, "order-98765", consignment["clientReference"])
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

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	consignments := bringRequireArray(t, *captured, "consignments", 1)
	consignment := consignments[0].(map[string]interface{})
	packages := bringRequireArray(t, consignment, "packages", 2)
	assert.Equal(t, float64(3.0), packages[0].(map[string]interface{})["weightInKg"])
	assert.Equal(t, float64(5.0), packages[1].(map[string]interface{})["weightInKg"])
}

func TestBringAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newBringTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)
	_, err := adapter.BookShipment(t.Context(), bringMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestBringAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		assert.Equal(t, "test@example.com", r.Header.Get("X-MyBring-API-Uid"))
		assert.Equal(t, "test-key", r.Header.Get("X-MyBring-API-Key"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bringMockTrackingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &BringAdapter{
		APIKey:     "test-key",
		CustomerID: "test@example.com",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.TrackShipment(t.Context(), "BREG00012345678DK")
	require.NoError(t, err)

	// Correct endpoint
	assert.Contains(t, capturedURL, "/tracking/api/v2/tracking.json")
	assert.Contains(t, capturedURL, "q=BREG00012345678DK")

	assert.Equal(t, "BREG00012345678DK", resp.TrackingNumber)
	assert.Equal(t, "The package is in transit", resp.Status)
	assert.Len(t, resp.Events, 2)
	assert.Equal(t, "Handed over to terminal", resp.Events[0].Details)
	assert.Equal(t, "Kolding, DK", resp.Events[0].Location)
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
	return `{
		"consignments": [
			{
				"confirmation": {
					"consignmentNumber": "BREG00012345678DK",
					"links": {
						"tracking": "https://sporing.bring.dk/tracking.html?q=BREG00012345678DK",
						"labels": "https://api.bring.com/booking/api/shipment/labels/PDF123"
					},
					"packages": [
						{"packageNumber": "70730254433219997", "correlationId": "0"}
					]
				}
			}
		]
	}`
}

func bringMockTrackingResponse() string {
	return `{
		"consignmentSet": [
			{
				"consignmentId": "BREG00012345678DK",
				"packageSet": [
					{
						"statusId": "IN_TRANSIT",
						"statusDescription": "The package is in transit",
						"eventSet": [
							{
								"description": "Handed over to terminal",
								"status": "IN_TRANSIT",
								"isoDateTime": "2026-06-07T16:45:00",
								"city": "Kolding",
								"countryCode": "DK"
							},
							{
								"description": "EDI notification received",
								"status": "REGISTERED",
								"isoDateTime": "2026-06-07T10:12:00"
							}
						]
					}
				]
			}
		]
	}`
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
		Name:        "Unisport Group",
		Street:      "Industrivej",
		HouseNumber: "10",
		City:        "Oslo",
		PostalCode:  "0123",
		Country:     "NO",
		Phone:       "+4712345678",
		Email:       "logistics@unisport.dk",
	}
}

func bringTestReceiver() Address {
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

func bringTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
		Items:      []Item{{Description: "Sports goods", Weight: weightKg, Quantity: 1}},
	}
}
