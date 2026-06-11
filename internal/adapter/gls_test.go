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
	"go.uber.org/zap"
)

// =========================================================================
// Mock adapter tests
// =========================================================================

func TestMockGLSAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockGLSAdapter{}).CancelShipment(t.Context(), "GLS123456789DK")
	require.NoError(t, err)
	assert.Equal(t, "GLS123456789DK", resp.TrackingNumber)
	assert.Equal(t, "cancelled", resp.Status)
	assert.Equal(t, "gls", resp.Carrier)
}

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
	assert.Equal(t, "Shipment Accepted", response.Status)
	assert.Equal(t, StatusUnknown, response.NormalizedStatus)
	assert.Len(t, response.Events, 1)
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
			TotalWeight: 2.5,
			Colli:       []Colli{glsTestColli("box-1", 2.5)},
		},
	})
	require.NoError(t, err)

	payload := *captured
	shipment := glsRequireNested(t, payload, "Shipment")

	// Product
	assert.Equal(t, "PARCEL", shipment["Product"])
	assert.NotEmpty(t, shipment["ShippingDate"])

	// Shipper
	shipper := glsRequireNested(t, shipment, "Shipper")
	assert.Equal(t, adapter.ContactID, shipper["ContactID"])
	shipperAddr := glsRequireNested(t, shipper, "Address")
	assert.Equal(t, "Unisport Group", shipperAddr["Name1"])
	assert.Equal(t, "Industrivej", shipperAddr["Street"])
	assert.Equal(t, "10", shipperAddr["StreetNumber"])
	assert.Equal(t, "2300", shipperAddr["Zipcode"])
	assert.Equal(t, "Copenhagen", shipperAddr["City"])
	assert.Equal(t, "DK", shipperAddr["CountryCode"])
	assert.Equal(t, "+4512345678", shipperAddr["MobilePhoneNumber"])
	assert.Equal(t, "logistics@unisport.dk", shipperAddr["Email"])

	// Consignee — must use PascalCase
	_, hasReceiver := shipment["receiver"]
	assert.False(t, hasReceiver, "GLS expects 'Consignee', not 'receiver'")
	consignee := glsRequireNested(t, shipment, "Consignee")
	assert.Equal(t, "PRIVATE", consignee["Category"])
	consigneeAddr := glsRequireNested(t, consignee, "Address")
	assert.Equal(t, "John Doe", consigneeAddr["Name1"])
	assert.Equal(t, "Storgatan", consigneeAddr["Street"])
	assert.Equal(t, "1", consigneeAddr["StreetNumber"])
	assert.Equal(t, "SE", consigneeAddr["CountryCode"])

	// ShipmentUnit
	units := glsRequireArray(t, shipment, "ShipmentUnit", 1)
	unit := units[0].(map[string]interface{})
	assert.Equal(t, float64(2.5), unit["Weight"])
	refs := unit["ShipmentUnitReference"].([]interface{})
	assert.Equal(t, "box-1", refs[0])

	// PrintingOptions
	opts := glsRequireNested(t, payload, "PrintingOptions")
	labels := glsRequireNested(t, opts, "ReturnLabels")
	assert.Equal(t, "PDF", labels["LabelFormat"])
}

func TestGLSAdapter_BookShipment_BusinessDelivery(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	req := glsMinimalRequest()
	req.Shipment.DeliveryType = "business"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")
	consignee := glsRequireNested(t, shipment, "Consignee")
	assert.Equal(t, "BUSINESS", consignee["Category"])
}

func TestGLSAdapter_BookShipment_ServicePoint(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	req := glsMinimalRequest()
	req.Shipment.Receiver = Address{
		Name:           "Recipient Name",
		Country:        "DK",
		Phone:          "+4587654321",
		ServicePointID: "DK-95763",
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")

	// Service block must be present with ShopDelivery
	services := shipment["Service"].([]interface{})
	require.Len(t, services, 1)
	svc := services[0].(map[string]interface{})
	shopDelivery := svc["ShopDelivery"].(map[string]interface{})
	assert.Equal(t, "DK-95763", shopDelivery["ParcelShopID"])
	assert.Equal(t, "ShopDelivery", shopDelivery["ServiceName"])
}

func TestGLSAdapter_BookShipment_HomeDelivery_NoServiceBlock(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")
	_, hasService := shipment["Service"]
	assert.False(t, hasService, "home delivery must not include a Service block")
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
	assert.Equal(t, float64(2.5), units[0].(map[string]interface{})["Weight"])
	assert.Equal(t, float64(5.0), units[1].(map[string]interface{})["Weight"])
}

func TestGLSAdapter_BookShipment_ContentTypeHeader(t *testing.T) {
	t.Parallel()

	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`))
			return
		}
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(glsMockBookingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		ContactID:    "test-contact",
		BaseURL:      srv.URL,
		AuthURL:      srv.URL + "/oauth2/v2/token",
		HTTPClient:   srv.Client(),
	}

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	require.NoError(t, err)
	assert.Equal(t, "application/glsVersion1+json", capturedContentType)
}

func TestGLSAdapter_BookShipment_IncotermsForwarded(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())

	req := glsMinimalRequest()
	req.Shipment.Customs.Incoterms = "DDP"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	_ = adapter
	shipment := glsRequireNested(t, *captured, "Shipment")
	assert.Equal(t, "DDP", shipment["IncotermCode"])
}

func TestGLSAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newGLSTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(t.Context(), glsMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestGLSAdapter_FetchLabel_UsesReprintParcel(t *testing.T) {
	t.Parallel()

	var capturedPath, capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CreatedShipment":{"PrintData":[{"LabelFormat":"PDF","Data":["JVBERi0xLjQ="]}]}}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ClientID: "test", ClientSecret: "secret", ContactID: "cid",
		BaseURL: srv.URL, AuthURL: srv.URL + "/oauth2/v2/token",
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.FetchLabel(t.Context(), LabelRequest{TrackingNumber: "GLS123", Format: LabelFormatPDF})
	require.NoError(t, err)

	assert.Equal(t, "/rs/shipments/reprintparcel", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "GLS123", resp.TrackingNumber)
	assert.Equal(t, "JVBERi0xLjQ=", resp.Data)
}

func TestGLSAdapter_FetchLabel_ZPLUsesZEBRA(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"CreatedShipment":{"PrintData":[{"LabelFormat":"ZEBRA","Data":["Wg=="]}]}}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ClientID: "test", ClientSecret: "secret", ContactID: "cid",
		BaseURL: srv.URL, AuthURL: srv.URL + "/oauth2/v2/token",
		HTTPClient: srv.Client(),
	}

	_, err := adapter.FetchLabel(t.Context(), LabelRequest{TrackingNumber: "GLS123", Format: LabelFormatZPL})
	require.NoError(t, err)

	opts := glsRequireNested(t, capturedBody, "PrintingOptions")
	labels := glsRequireNested(t, opts, "ReturnLabels")
	assert.Equal(t, "ZEBRA", labels["LabelFormat"], "LabelFormat must be ZEBRA not ZPL")
	assert.Equal(t, "ZPL_200", labels["TemplateSet"])
}

func TestGLSAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	var capturedPath, capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"TrackID":"GLS123","Result":"CANCELLED"}`))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ClientID: "test", ClientSecret: "secret", ContactID: "cid",
		BaseURL: srv.URL, AuthURL: srv.URL + "/oauth2/v2/token",
		HTTPClient: srv.Client(),
	}

	resp, err := adapter.CancelShipment(t.Context(), "GLS123")
	require.NoError(t, err)

	assert.Equal(t, "/rs/shipments/cancel/GLS123", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "cancelled", resp.Status)
	assert.Equal(t, "GLS123", resp.TrackingNumber)
}

func TestGLSAdapter_BookShipment_UnsupportedAddOns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		addOn  AddOn
		errMsg string
	}{
		{"sms notification", AddOn{Type: AddOnSMSNotification}, "SMS notification add-on"},
		{"email notification outbound", AddOn{Type: AddOnEmailNotification}, "email notification add-on"},
		{"flex delivery", AddOn{Type: AddOnFlexDelivery}, "flex delivery add-on"},
		{"signature required", AddOn{Type: AddOnSignatureRequired}, "signature required add-on"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter, _ := newGLSTestServer(t, http.StatusOK, glsMockBookingResponse())
			req := glsMinimalRequest()
			req.Shipment.AddOns = []AddOn{tc.addOn}
			_, err := adapter.BookShipment(t.Context(), req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestGLSAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`))
			return
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &capturedBody))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(glsMockTrackingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &GLSAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		ContactID:    "test-contact",
		BaseURL:      srv.URL,
		AuthURL:      srv.URL + "/oauth2/v2/token",
		HTTPClient:   srv.Client(),
	}

	resp, err := adapter.TrackShipment(t.Context(), "GLS123")
	require.NoError(t, err)
	assert.Equal(t, "GLS123", resp.TrackingNumber)
	assert.NotEmpty(t, resp.Events)

	// DetailsReferenceData only carries TrackID — no DateFrom/DateTo.
	assert.Equal(t, "GLS123", capturedBody["TrackID"])
	assert.Empty(t, capturedBody["DateFrom"], "DateFrom must not be sent to /rs/tracking/parceldetails")
	assert.Empty(t, capturedBody["DateTo"], "DateTo must not be sent to /rs/tracking/parceldetails")
}

// =========================================================================
// Helpers
// =========================================================================

// newGLSTestServer starts an httptest.Server that handles both the OAuth token
// endpoint and the shipment endpoint. Captures the shipment request body.
func newGLSTestServer(t *testing.T, statusCode int, body string) (*GLSAdapter, *map[string]interface{}) {
	t.Helper()

	var captured map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle OAuth token requests separately.
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`))
			return
		}
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &captured))
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return &GLSAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		ContactID:    "test-contact-id",
		BaseURL:      srv.URL,
		AuthURL:      srv.URL + "/oauth2/v2/token",
		HTTPClient:   srv.Client(),
	}, &captured
}

// glsMockBookingResponse returns a minimal valid GLS booking response.
func glsMockBookingResponse() string {
	return `{
		"CreatedShipment": {
			"CustomerID": "test",
			"PickupLocation": "DK8000",
			"ParcelData": [
				{"TrackID": "GLS123456789DK", "ParcelNumber": "370730254433"}
			],
			"PrintData": [
				{"LabelFormat": "PDF", "Data": ["JVBERi0xLjQ="]}
			]
		}
	}`
}

// glsMockTrackingResponse returns a minimal valid GLS tracking response.
func glsMockTrackingResponse() string {
	return `{
		"UnitDetail": {
			"TrackID": "GLS123",
			"Weight": 2.4,
			"Product": "PARCEL",
			"History": [
				{
					"Date": "2026-06-07T16:45:00Z",
					"LocationCode": "DK0022",
					"Location": "Kolding Hub",
					"Country": "DK",
					"StatusCode": "001",
					"Description": "Parcel arrived at sorting terminal."
				}
			]
		}
	}`
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

func glsTestReceiver() Address {
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

func glsTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
		Items:      []Item{{Description: "Sports goods", Weight: weightKg, Quantity: 1}},
	}
}

// =========================================================================
// Return shipment tests
// =========================================================================

// newGLSReturnTestServer starts an httptest.Server for the GLS Shop Returns
// Customer Plus API v3. Captures the return-order request body.
func newGLSReturnTestServer(t *testing.T, appID string) (*GLSAdapter, *map[string]interface{}) {
	t.Helper()

	var captured map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/v2/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &captured))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"returnOrderId": "RO-12345",
			"references": {"trackId": "GLS-RET-9999", "parcelId": "P-001"},
			"label": {"contentType": "application/pdf", "content": "JVBERi0xLjQ="}
		}`))
	}))
	t.Cleanup(srv.Close)

	log, _ := zap.NewDevelopment()
	adapter := &GLSAdapter{
		ClientID:      "test-client",
		ClientSecret:  "test-secret",
		ContactID:     "test-contact-id",
		ReturnAppID:   appID,
		BaseURL:       srv.URL,
		ReturnBaseURL: srv.URL,
		AuthURL:       srv.URL + "/oauth2/v2/token",
		HTTPClient:    srv.Client(),
		log:           log,
	}
	return adapter, &captured
}

func TestGLSAdapter_BookShipment_Return_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSReturnTestServer(t, "APP-42")

	req := BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			DeliveryType: "return",
			Sender:       glsTestSender(),
			Receiver: Address{
				Name:       "Returning Customer",
				Street:     "Storgatan",
				HouseNumber: "1",
				City:       "Stockholm",
				PostalCode: "111 22",
				Country:    "SE",
				Email:      "customer@example.com",
			},
			TotalWeight: 2.0,
			Colli:       []Colli{glsTestColli("order-ref-001", 2.0)},
		},
	}

	resp, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	// Response fields
	assert.Equal(t, "GLS-RET-9999", resp.TrackingNumber)
	assert.Equal(t, "RO-12345", resp.ShipmentID)
	assert.Equal(t, "gls", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)

	// Payload shape
	payload := *captured

	// originalOrderReference must come from the first colli ID
	assert.Equal(t, "order-ref-001", payload["originalOrderReference"])
	assert.Equal(t, "OTHER", payload["returnReason"])

	// Sender is the customer (our Receiver) returning the parcel
	sender := payload["sender"].(map[string]interface{})
	assert.Equal(t, "Returning Customer", sender["personName"])
	addr := sender["address"].(map[string]interface{})
	assert.Equal(t, "Storgatan", addr["street"])
	assert.Equal(t, "111 22", addr["zipCode"])
	assert.Equal(t, "SE", addr["countryCode"])

	// Receiver is the merchant (our Sender)
	receiver := payload["receiver"].(map[string]interface{})
	assert.Equal(t, "Unisport Group", receiver["companyName"])
	recvAddr := receiver["address"].(map[string]interface{})
	assert.Equal(t, "DK", recvAddr["countryCode"])

	// Label format must be lowercase
	assert.Equal(t, "pdf", payload["labelFormat"])
}

func TestGLSAdapter_BookShipment_Return_EmailNotification(t *testing.T) {
	t.Parallel()

	adapter, captured := newGLSReturnTestServer(t, "APP-42")

	req := BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			DeliveryType: "return",
			Sender:       glsTestSender(),
			Receiver: Address{
				Name:       "Customer",
				Street:     "Storgatan",
				HouseNumber: "1",
				City:       "Stockholm",
				PostalCode: "111 22",
				Country:    "SE",
				Email:      "notify@example.com",
			},
			TotalWeight: 1.0,
			Colli:       []Colli{glsTestColli("ref-002", 1.0)},
			AddOns:      []AddOn{{Type: AddOnEmailNotification}},
		},
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	payload := *captured
	opts, ok := payload["options"].(map[string]interface{})
	require.True(t, ok, "options must be present when AddOnEmailNotification is set")
	mail := opts["confirmationMail"].(map[string]interface{})
	assert.Equal(t, "notify@example.com", mail["sendTo"])
}

func TestGLSAdapter_BookShipment_Return_MissingAppID(t *testing.T) {
	t.Parallel()

	adapter, _ := newGLSReturnTestServer(t, "") // empty ReturnAppID

	req := BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			DeliveryType: "return",
			Sender:       glsTestSender(),
			Receiver:     glsTestReceiver(),
			TotalWeight:  1.0,
			Colli:        []Colli{glsTestColli("ref-003", 1.0)},
		},
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReturnAppID must be configured")
}
