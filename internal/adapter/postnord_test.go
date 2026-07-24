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
	assert.Equal(t, "IN_TRANSPORT", response.Status)
	assert.Equal(t, StatusInTransit, response.NormalizedStatus)
	assert.Len(t, response.Events, 2)
}

// =========================================================================
// Real adapter — payload transformation tests
// =========================================================================

func TestPostNordAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	resp, err := adapter.BookShipment(t.Context(), BookingRequest{
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

	payload := *captured

	// Top-level required fields
	assert.NotEmpty(t, payload["messageDate"])
	assert.Equal(t, "Instruction", payload["messageFunction"])
	assert.NotEmpty(t, payload["messageId"])
	assert.Equal(t, "Original", payload["updateIndicator"])

	// CarrierMessageID must be returned to the caller so it can be reused on
	// a later UpdateShipment call — see APIdocs/postnord_update_cancel.rtf.
	assert.Equal(t, payload["messageId"], resp.CarrierMessageID)

	// application block
	app := requireNested(t, payload, "application")
	assert.Equal(t, float64(adapter.ApplicationID), app["applicationId"])

	// shipment array
	shipments, ok := payload["shipment"].([]any)
	require.True(t, ok, "payload missing 'shipment' array")
	require.Len(t, shipments, 1)
	shipment := shipments[0].(map[string]any)

	// service — basicServiceCode not serviceId
	service := requireNested(t, shipment, "service")
	assert.Equal(t, "18", service["basicServiceCode"], "home delivery should use code 18")
	assert.NotContains(t, service, "serviceId", "v3 uses basicServiceCode not serviceId")

	// parties
	parties := requireNested(t, shipment, "parties")

	consignor := requireNested(t, parties, "consignor")
	assert.Equal(t, "Z11", consignor["issuerCode"]) // DK
	consignorPartyID := requireNested(t, consignor, "partyIdentification")
	assert.Equal(t, adapter.CustomerNumber, consignorPartyID["partyId"])
	assert.Equal(t, "160", consignorPartyID["partyIdType"])
	consignorParty := requireNested(t, consignor, "party")
	consignorName := requireNested(t, consignorParty, "nameIdentification")
	assert.Equal(t, "Unisport Group", consignorName["name"])
	consignorAddr := requireNested(t, consignorParty, "address")
	streets := consignorAddr["streets"].([]any)
	assert.Equal(t, "Industrivej 10", streets[0])
	assert.Equal(t, "DK", consignorAddr["countryCode"])

	consignee := requireNested(t, parties, "consignee")
	consigneeParty := requireNested(t, consignee, "party")
	consigneeName := requireNested(t, consigneeParty, "nameIdentification")
	assert.Equal(t, "John Doe", consigneeName["name"])

	// goodsItem — weight in kg with unit KGM
	goodsItems, ok := shipment["goodsItem"].([]any)
	require.True(t, ok)
	require.Len(t, goodsItems, 1)
	item := goodsItems[0].(map[string]any)
	assert.Equal(t, "PC", item["packageTypeCode"])
	items := item["items"].([]any)
	require.Len(t, items, 1)
	weight := requireNested(t, items[0].(map[string]any), "grossWeight")
	assert.Equal(t, 1.5, weight["value"])
	assert.Equal(t, "KGM", weight["unit"])

	// totalGrossWeight at shipment level
	totalWeight := requireNested(t, shipment, "totalGrossWeight")
	assert.Equal(t, 1.5, totalWeight["value"])
	assert.Equal(t, "KGM", totalWeight["unit"])
}

func TestPostNordAdapter_BookShipment_ServicePoint(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.Shipment.Receiver = Address{
		Name:           "Anna Svensson",
		Country:        "SE",
		Phone:          "+46701234567",
		ServicePointID: "9814",
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipments := (*captured)["shipment"].([]any)
	shipment := shipments[0].(map[string]any)

	// Service code 19 for service point
	service := requireNested(t, shipment, "service")
	assert.Equal(t, "19", service["basicServiceCode"])

	// deliveryParty block with partyIdType 156
	parties := requireNested(t, shipment, "parties")
	deliveryParty := requireNested(t, parties, "deliveryParty")
	partyID := requireNested(t, deliveryParty, "partyIdentification")
	assert.Equal(t, "9814", partyID["partyId"])
	assert.Equal(t, "156", partyID["partyIdType"])
}

func TestPostNordAdapter_BookShipment_Notifications(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.Shipment.Receiver.Phone = "+4587654321"
	req.Shipment.Receiver.Email = "receiver@example.com"
	req.Shipment.AddOns = []AddOn{
		{Type: AddOnSMSNotification},
		{Type: AddOnEmailNotification},
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipments := (*captured)["shipment"].([]any)
	shipment := shipments[0].(map[string]any)
	parties := requireNested(t, shipment, "parties")
	consignee := requireNested(t, parties, "consignee")
	consigneeParty := requireNested(t, consignee, "party")
	contact := requireNested(t, consigneeParty, "contact")

	// Notifications via contact fields, not options array
	assert.Equal(t, "+4587654321", contact["smsNo"])
	assert.Equal(t, "receiver@example.com", contact["emailAddress"])
	assert.NotContains(t, shipment, "options", "v3 uses contact fields for notifications, not options array")

	_ = adapter
}

func TestPostNordAdapter_BookShipment_SignatureAndInsurance(t *testing.T) {
	t.Parallel()

	t.Run("signature_required maps to A2", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

		req := postnordMinimalRequest()
		req.Shipment.AddOns = []AddOn{{Type: AddOnSignatureRequired}}

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)

		shipments := (*captured)["shipment"].([]any)
		shipment := shipments[0].(map[string]any)
		service := requireNested(t, shipment, "service")
		codes := service["additionalServiceCode"].([]any)
		assert.Contains(t, codes, "A2")
		_ = adapter
	})

	t.Run("insurance maps to A8", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

		req := postnordMinimalRequest()
		req.Shipment.AddOns = []AddOn{{
			Type:              AddOnInsurance,
			InsuranceValue:    5000.0,
			InsuranceCurrency: "DKK",
		}}

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)

		shipments := (*captured)["shipment"].([]any)
		shipment := shipments[0].(map[string]any)
		service := requireNested(t, shipment, "service")
		codes := service["additionalServiceCode"].([]any)
		assert.Contains(t, codes, "A8")
		_ = adapter
	})

	t.Run("insurance requires InsuranceValue > 0", func(t *testing.T) {
		t.Parallel()
		adapter, _ := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

		req := postnordMinimalRequest()
		req.Shipment.AddOns = []AddOn{{Type: AddOnInsurance}}

		_, err := adapter.BookShipment(t.Context(), req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "InsuranceValue")
		_ = adapter
	})

	t.Run("A2 and A8 combined", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

		req := postnordMinimalRequest()
		req.Shipment.AddOns = []AddOn{
			{Type: AddOnSignatureRequired},
			{Type: AddOnInsurance, InsuranceValue: 1000.0, InsuranceCurrency: "DKK"},
		}

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)

		shipments := (*captured)["shipment"].([]any)
		shipment := shipments[0].(map[string]any)
		service := requireNested(t, shipment, "service")
		codes := service["additionalServiceCode"].([]any)
		assert.Contains(t, codes, "A2")
		assert.Contains(t, codes, "A8")
		_ = adapter
	})
}

func TestPostNordAdapter_BookShipment_APIKeyInQueryParam(t *testing.T) {
	t.Parallel()

	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(postnordMockBookingResponse()))
	}))
	t.Cleanup(srv.Close)

	adapter := &PostNordAdapter{
		APIKey:         "test-api-key",
		CustomerNumber: "150011208",
		ApplicationID:  1438,
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
	}

	_, err := adapter.BookShipment(t.Context(), postnordMinimalRequest())
	require.NoError(t, err)
	assert.Contains(t, capturedURL, "apikey=test-api-key")
	assert.Contains(t, capturedURL, "/rest/shipment/v3/edi/labels/pdf")
}

func TestPostNordAdapter_BookShipment_IdempotencyKey(t *testing.T) {
	t.Parallel()

	adapter, captured := newPostNordTestServer(t, http.StatusOK, postnordMockBookingResponse())

	req := postnordMinimalRequest()
	req.IdempotencyKey = "order-98765"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	shipments := (*captured)["shipment"].([]any)
	shipment := shipments[0].(map[string]any)
	refs := shipment["references"].([]any)
	ref := refs[0].(map[string]any)
	assert.Equal(t, "order-98765", ref["referenceNo"])
	assert.Equal(t, "CU", ref["referenceType"])

	_ = adapter
}

func TestPostNordAdapter_BookShipment_Return(t *testing.T) {
	t.Parallel()

	t.Run("return routes to returns endpoint", func(t *testing.T) {
		t.Parallel()
		var capturedURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(postnordMockBookingResponse()))
		}))
		t.Cleanup(srv.Close)

		adapter := &PostNordAdapter{
			APIKey: "test-key", CustomerNumber: "150011208",
			ApplicationID: 1438, BaseURL: srv.URL, HTTPClient: srv.Client(),
		}

		req := postnordMinimalRequest()
		req.Shipment.DeliveryType = "return"

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)
		assert.Contains(t, capturedURL, "/rest/shipment/v3/returns/edi/labels/pdf")
		assert.Contains(t, capturedURL, "functionality=STANDARD")
	})

	t.Run("labelless functionality", func(t *testing.T) {
		t.Parallel()
		var capturedURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(postnordMockBookingResponse()))
		}))
		t.Cleanup(srv.Close)

		adapter := &PostNordAdapter{
			APIKey: "test-key", CustomerNumber: "150011208",
			ApplicationID: 1438, BaseURL: srv.URL, HTTPClient: srv.Client(),
		}

		req := postnordMinimalRequest()
		req.Shipment.DeliveryType = "return"
		req.Shipment.ReturnFunctionality = "labelless"

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)
		assert.Contains(t, capturedURL, "functionality=LABELLESS")
	})

	t.Run("SMS add-on adds smsQRcode query param", func(t *testing.T) {
		t.Parallel()
		var capturedURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(postnordMockBookingResponse()))
		}))
		t.Cleanup(srv.Close)

		adapter := &PostNordAdapter{
			APIKey: "test-key", CustomerNumber: "150011208",
			ApplicationID: 1438, BaseURL: srv.URL, HTTPClient: srv.Client(),
		}

		req := postnordMinimalRequest()
		req.Shipment.DeliveryType = "return"
		req.Shipment.Receiver.Phone = "+4587654321"
		req.Shipment.AddOns = []AddOn{{Type: AddOnSMSNotification}}

		_, err := adapter.BookShipment(t.Context(), req)
		require.NoError(t, err)
		assert.Contains(t, capturedURL, "smsQRcode=true")
	})

	t.Run("regular booking does not use returns endpoint", func(t *testing.T) {
		t.Parallel()
		var capturedURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedURL = r.URL.String()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(postnordMockBookingResponse()))
		}))
		t.Cleanup(srv.Close)

		adapter := &PostNordAdapter{
			APIKey: "test-key", CustomerNumber: "150011208",
			ApplicationID: 1438, BaseURL: srv.URL, HTTPClient: srv.Client(),
		}

		_, err := adapter.BookShipment(t.Context(), postnordMinimalRequest())
		require.NoError(t, err)
		assert.NotContains(t, capturedURL, "/returns/")
		assert.Contains(t, capturedURL, "/rest/shipment/v3/edi/labels/pdf")
	})
}

func TestPostNordAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newPostNordTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(t.Context(), postnordMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")

	_ = adapter
}

func TestPostNordAdapter_SupportsNativeIdempotency(t *testing.T) {
	t.Parallel()
	assert.True(t, SupportsNativeIdempotency("postnord"))
}

// =========================================================================
// UpdateShipment — messageId reuse (APIdocs/postnord_update_cancel.rtf)
// =========================================================================

func TestPostNordAdapter_UpdateShipment_MessageID(t *testing.T) {
	t.Parallel()

	t.Run("reuses caller-supplied CarrierMessageID", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusOK, "{}")

		resp, err := adapter.UpdateShipment(t.Context(), UpdateRequest{
			TrackingNumber:   "00073215400599388772",
			ReceiverPhone:    "+4587654321",
			CarrierMessageID: "msg-original-booking-123",
		})
		require.NoError(t, err)
		assert.Equal(t, "updated", resp.Status)

		payload := *captured
		assert.Equal(t, "msg-original-booking-123", payload["messageId"])
		assert.Equal(t, "Update", payload["updateIndicator"])
	})

	t.Run("generates a messageId when none supplied", func(t *testing.T) {
		t.Parallel()
		adapter, captured := newPostNordTestServer(t, http.StatusOK, "{}")

		_, err := adapter.UpdateShipment(t.Context(), UpdateRequest{
			TrackingNumber: "00073215400599388772",
			ReceiverEmail:  "new@example.com",
		})
		require.NoError(t, err)

		payload := *captured
		assert.NotEmpty(t, payload["messageId"])
	})
}

// =========================================================================
// Helpers
// =========================================================================

// requireNested extracts a nested map by key, failing the test if absent.
func requireNested(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := parent[key].(map[string]any)
	require.True(t, ok, "object missing nested '%s' key", key)
	return nested
}

func postnordMockBookingResponse() string {
	return `{
		"bookingResponse": {
			"bookingId": "5Y9SR0CSAO9MTJAYCS8YM3RLZY40LI",
			"idInformation": [
				{
					"status": "OK",
					"ids": [
						{"idType": "itemId", "value": "00573132900000558136", "printId": "abc123"}
					],
					"urls": [
						{"type": "TRACKING", "url": "https://tracking.postnord.com/?id=00573132900000558136"}
					]
				}
			]
		},
		"labelPrintout": [
			{
				"itemIds": [{"itemIds": "00573132900000558136", "status": "OK"}],
				"printout": {
					"labelFormat": "PDF",
					"encoding": "base64",
					"data": "JVBERi0xLjQ=",
					"type": "LABEL"
				}
			}
		]
	}`
}

func newPostNordTestServer(t *testing.T, statusCode int, body string) (*PostNordAdapter, *map[string]any) {
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

	return &PostNordAdapter{
		APIKey:         "test-key",
		CustomerNumber: "150011208",
		ApplicationID:  1438,
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
	}, &captured
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
