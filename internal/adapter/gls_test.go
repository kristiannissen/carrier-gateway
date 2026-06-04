// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/gls_test.go.
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

func TestMockGLSAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("missing TotalWeight", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockGLSAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "gls",
			Shipment: Shipment{
				Sender:   glsTestSender(),
				Receiver: glsTestReceiver(),
				Colli:    []Colli{glsTestColli("colli-1", 5.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight is required and must be greater than 0")
	})

	t.Run("TotalWeight mismatch", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockGLSAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "gls",
			Shipment: Shipment{
				Sender:      glsTestSender(),
				Receiver:    glsTestReceiver(),
				TotalWeight: 3.0,
				Colli:       []Colli{glsTestColli("colli-1", 5.0)},
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TotalWeight must match the sum of all colli weights")
	})

	t.Run("valid request", func(t *testing.T) {
		t.Parallel()
		response, err := (&MockGLSAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "gls",
			Shipment: Shipment{
				Sender:      glsTestSender(),
				Receiver:    glsTestReceiver(),
				TotalWeight: 5.0,
				Colli:       []Colli{glsTestColli("colli-1", 5.0)},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "gls", response.Carrier)
		assert.NotEmpty(t, response.TrackingNumber)
		assert.NotEmpty(t, response.LabelURL)
	})
}

func TestMockGLSAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	response, err := (&MockGLSAdapter{}).TrackShipment(t.Context(), "GLS123456789DK")
	require.NoError(t, err)
	assert.Equal(t, "GLS123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestMockGLSAdapter_GetServicePoints(t *testing.T) {
	t.Parallel()

	servicePoints, err := (&MockGLSAdapter{}).GetServicePoints(t.Context(), Location{
		City: "Copenhagen", Country: "DK",
	})
	require.NoError(t, err)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "GLS001", servicePoints[0].ID)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestGLSAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), BookingRequest{
		Carrier: "gls",
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
			TotalWeight: 2.5,
			Colli:       []Colli{glsTestColli("box-1", 2.5)},
		},
	})
	require.NoError(t, err)

	payload := *captured

	// Top-level must have Shipment and PrintingOptions
	shipment := glsRequireNested(t, payload, "Shipment")
	glsRequireNested(t, payload, "PrintingOptions")

	// Product
	assert.Equal(t, "PARCEL", shipment["Product"])

	// Shipper — must have ContactID and nested Address
	shipper := glsRequireNested(t, shipment, "Shipper")
	assert.Equal(t, adapter.ContactID, shipper["ContactID"])
	shipperAddr := glsRequireNested(t, shipper, "Address")
	assert.Equal(t, "Unisport Group", shipperAddr["Name1"])
	assert.Equal(t, "Industrivej 10", shipperAddr["Street"])
	assert.Equal(t, "2300", shipperAddr["Zipcode"]) // PostalCode → Zipcode
	assert.Equal(t, "Copenhagen", shipperAddr["City"])
	assert.Equal(t, "DK", shipperAddr["CountryCode"]) // Country → CountryCode
	assert.Equal(t, "+4512345678", shipperAddr["MobilePhoneNumber"])
	assert.Equal(t, "logistics@unisport.dk", shipperAddr["Email"])

	// Consignee — must not be called "receiver" or "recipient"
	_, hasReceiver := shipment["receiver"]
	assert.False(t, hasReceiver, "GLS expects 'Consignee', not 'receiver'")
	_, hasRecipient := shipment["recipient"]
	assert.False(t, hasRecipient, "GLS expects 'Consignee', not 'recipient'")
	consignee := glsRequireNested(t, shipment, "Consignee")
	consigneeAddr := glsRequireNested(t, consignee, "Address")
	assert.Equal(t, "John Doe", consigneeAddr["Name1"])
	assert.Equal(t, "SE", consigneeAddr["CountryCode"])

	// ShipmentUnit
	units := glsRequireArray(t, shipment, "ShipmentUnit", 1)
	unit := units[0].(map[string]interface{})
	assert.Equal(t, float64(2.5), unit["Weight"]) // kg, no conversion
	refs := unit["ShipmentUnitReference"].([]interface{})
	assert.Equal(t, "box-1", refs[0]) // colli.ID forwarded as reference
}

func TestGLSAdapter_BookShipment_VolumeIncluded(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	require.NoError(t, err)

	shipment := glsRequireNested(t, *captured, "Shipment")
	units := glsRequireArray(t, shipment, "ShipmentUnit", 1)
	unit := units[0].(map[string]interface{})

	volume := glsRequireNested(t, unit, "Volume")
	assert.Equal(t, "10", volume["Length"])
	assert.Equal(t, "10", volume["Width"])
	assert.Equal(t, "10", volume["Height"])
	assert.Equal(t, "NON_CALIBRATED", volume["VolumetricType"])

	_ = adapter // suppress unused warning
}

func TestGLSAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	req := glsMinimalRequest()
	req.Shipment.TotalWeight = 7.5
	req.Shipment.Colli = []Colli{
		glsTestColli("box-1", 2.5),
		glsTestColli("box-2", 5.0),
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")
	units := glsRequireArray(t, shipment, "ShipmentUnit", 2)

	u0 := units[0].(map[string]interface{})
	assert.Equal(t, float64(2.5), u0["Weight"])

	u1 := units[1].(map[string]interface{})
	assert.Equal(t, float64(5.0), u1["Weight"])
}

func TestGLSAdapter_BookShipment_IncotermsForwarded(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	req := glsMinimalRequest()
	req.Shipment.Incoterms = "DDP"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")
	assert.Equal(t, "DDP", shipment["IncotermCode"])
}

func TestGLSAdapter_BookShipment_PrintingOptionsPresent(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	require.NoError(t, err)

	_ = adapter
	opts := glsRequireNested(t, *captured, "PrintingOptions")
	labels := glsRequireNested(t, opts, "ReturnLabels")
	assert.Equal(t, "NONE", labels["TemplateSet"])
	assert.Equal(t, "PDF", labels["LabelFormat"])
}

func TestGLSAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newGLSTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestGLSAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"UnitItems":[{"TrackID":"GLS123","Status":"In Transit","InitialDate":"2026-01-01T10:00:00Z"}]}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ContactID:  "test-contact",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.TrackShipment(t.Context(), "GLS123")
	require.NoError(t, err)
	assert.Equal(t, "GLS123", resp.TrackingNumber)
	assert.Equal(t, "In Transit", resp.Status)
	assert.Len(t, resp.Events, 1)

	// Verify tracking uses POST with TrackID in body
	assert.Equal(t, "GLS123", captured["TrackID"])
	assert.NotEmpty(t, captured["DateFrom"])
	assert.NotEmpty(t, captured["DateTo"])
}

func TestGLSAdapter_GetServicePoints_RequestShape(t *testing.T) {
	t.Parallel()

	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ParcelShop":[{"ParcelShopID":"GLS001","Address":{"Name1":"GLS Copenhagen","Street":"Main St 1","Zipcode":"1234","City":"Copenhagen","CountryCode":"DK"}}]}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ContactID:  "test-contact",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	points, err := adapter.GetServicePoints(t.Context(), Location{
		City: "Copenhagen", Country: "DK", PostalCode: "1234",
	})
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.Equal(t, "GLS001", points[0].ID)
	assert.Equal(t, "1234", points[0].Address.PostalCode) // Zipcode → PostalCode

	// Verify request uses POST with location body
	assert.Equal(t, "DK", captured["CountryCode"])
	assert.Equal(t, "Copenhagen", captured["City"])
	assert.Equal(t, "1234", captured["Zipcode"])
}

// =========================================================================
// Helpers
// =========================================================================

// newGLSTestServer starts an httptest.Server that captures the request body
// and responds with statusCode and body.
func newGLSTestServer(t *testing.T, statusCode int, body string) (*GLSAdapter, *map[string]interface{}) {
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

	return &GLSAdapter{
		ContactID:  "test-contact-id",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}, &captured
}

// glsMockBookingResponse returns a minimal valid GLS CreateParcelsResponse.
func glsMockBookingResponse() string {
	return `{"CreatedShipment":{"CustomerID":"test","PickupLocation":"CPH","ParcelData":[{"TrackID":"GLS123456789DK","ParcelNumber":"PN001"}],"PrintData":[{"Data":["https://mock.gls-group.eu/labels/GLS123456789DK.pdf"],"LabelFormat":"PDF"}]}}`
}

func glsRequireNested(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	v, ok := parent[key].(map[string]interface{})
	require.True(t, ok, "missing nested key %q", key)
	return v
}

func glsRequireArray(t *testing.T, parent map[string]interface{}, key string, wantLen int) []interface{} {
	t.Helper()
	v, ok := parent[key].([]interface{})
	require.True(t, ok, "missing array key %q", key)
	require.Len(t, v, wantLen)
	return v
}

func glsMinimalRequest() BookingRequest {
	return BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			Sender:      glsTestSender(),
			Receiver:    glsTestReceiver(),
			TotalWeight: 1.0,
			Colli:       []Colli{glsTestColli("c1", 1.0)},
		},
	}
}

func glsTestSender() Address {
	return Address{
		Name: "Sender", Street: "Street 1",
		City: "Copenhagen", PostalCode: "2300", Country: "DK",
	}
}

func glsTestReceiver() Address {
	return Address{
		Name: "Receiver", Street: "Street 2",
		City: "Stockholm", PostalCode: "111 22", Country: "SE",
	}
}

func glsTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
	}
}
