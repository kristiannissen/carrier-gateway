// Package adapter provides carrier-specific customs declaration tests.
// This file is located at /internal/adapter/customs_test.go.
package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func testCustomsRequest() CustomsRequest {
	return CustomsRequest{
		TrackingNumber:     "1234567890",
		EDIItemID:          "ITEM-001",
		OriginCountry:      "DK",
		DestinationCountry: "NO",
		Customs: Customs{
			Incoterms:         "DDP",
			HSCode:            "61091000",
			CustomsValue:      500.0,
			CustomsCurrency:   "DKK",
			ExporterVATNumber: "DE123456789",
			ImporterVATNumber: "NO123456789",
			ShipmentType:      "B2B",
			Items: []CustomsItem{
				{
					Description:     "Cotton T-shirts",
					HSCode:          "61091000",
					CountryOfOrigin: "CN",
					Quantity:        10,
					NetWeight:       2.5,
					Value:           500.0,
					Currency:        "DKK",
				},
			},
		},
		Sender: Address{
			Name:       "Acme ApS",
			Street:     "Nørrebrogade",
			HouseNumber: "42",
			City:       "Copenhagen",
			PostalCode: "2200",
			Country:    "DK",
		},
		Receiver: Address{
			Name:       "Oslo Goods AS",
			Street:     "Karl Johans gate",
			HouseNumber: "1",
			City:       "Oslo",
			PostalCode: "0154",
			Country:    "NO",
		},
	}
}

// ─── buildGLSLineItems ────────────────────────────────────────────────────────

func TestBuildGLSLineItems_FromItems(t *testing.T) {
	t.Parallel()

	c := Customs{
		HSCode:          "00000000",
		CustomsValue:    999.0,
		CustomsCurrency: "EUR",
		Items: []CustomsItem{
			{HSCode: "61091000", CountryOfOrigin: "CN", Value: 100.0, Currency: "DKK", Quantity: 2, Description: "T-shirts"},
			{HSCode: "84713000", CountryOfOrigin: "US", Value: 200.0, Currency: "USD", Quantity: 1, Description: "Laptop"},
		},
	}
	items := buildGLSLineItems(c)
	require.Len(t, items, 2)
	assert.Equal(t, "61091000", items[0].CommodityCode)
	assert.Equal(t, "CN", items[0].CountryOfOrigin)
	assert.Equal(t, 100.0, items[0].ValueInInvoiceCurrency)
	assert.Equal(t, "DKK", items[0].InvoiceCurrency)
	assert.Equal(t, 2, items[0].Quantity)
}

func TestBuildGLSLineItems_FallbackToTopLevel(t *testing.T) {
	t.Parallel()

	c := Customs{
		HSCode:          "61091000",
		CountryOfOrigin: "CN",
		CustomsValue:    500.0,
		CustomsCurrency: "DKK",
	}
	items := buildGLSLineItems(c)
	require.Len(t, items, 1)
	assert.Equal(t, "61091000", items[0].CommodityCode)
	assert.Equal(t, 500.0, items[0].ValueInInvoiceCurrency)
}

func TestBuildGLSLineItems_CurrencyFallback(t *testing.T) {
	t.Parallel()

	// Item has no Currency — should fall back to Customs.CustomsCurrency.
	c := Customs{
		CustomsCurrency: "EUR",
		Items: []CustomsItem{
			{HSCode: "61091000", Value: 100.0, Quantity: 1},
		},
	}
	items := buildGLSLineItems(c)
	require.Len(t, items, 1)
	assert.Equal(t, "EUR", items[0].InvoiceCurrency)
}

// ─── buildPostNordItems ───────────────────────────────────────────────────────

func TestBuildPostNordItems_FromItems(t *testing.T) {
	t.Parallel()

	c := Customs{
		CustomsCurrency: "EUR",
		Items: []CustomsItem{
			{HSCode: "61091000", CountryOfOrigin: "CN", Quantity: 5, NetWeight: 1.2, Value: 100.0, Currency: "DKK"},
		},
	}
	items, warnings := buildPostNordItems(c)
	require.Len(t, items, 1)
	assert.Empty(t, warnings)
	assert.Equal(t, "61091000", items[0].HSTariffNumber)
	assert.Equal(t, 5, items[0].Quantity)
	assert.Equal(t, 1.2, items[0].NetWeight)
	assert.Equal(t, "DKK", items[0].Currency)
}

func TestBuildPostNordItems_TruncatesAt5(t *testing.T) {
	t.Parallel()

	c := Customs{
		CustomsCurrency: "EUR",
		Items: make([]CustomsItem, 7),
	}
	for i := range c.Items {
		c.Items[i] = CustomsItem{HSCode: "61091000", Value: 10.0, Quantity: 1}
	}
	items, warnings := buildPostNordItems(c)
	assert.Len(t, items, maxPostNordItems)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "truncated")
}

func TestBuildPostNordItems_FallbackToTopLevel(t *testing.T) {
	t.Parallel()

	c := Customs{
		HSCode:          "61091000",
		CountryOfOrigin: "CN",
		CustomsValue:    500.0,
		CustomsCurrency: "DKK",
	}
	items, warnings := buildPostNordItems(c)
	require.Len(t, items, 1)
	assert.Empty(t, warnings)
	assert.Equal(t, "61091000", items[0].HSTariffNumber)
	assert.Equal(t, 500.0, items[0].ItemValue)
}

func TestBuildPostNordItems_CurrencyFallback(t *testing.T) {
	t.Parallel()

	c := Customs{
		CustomsCurrency: "EUR",
		Items: []CustomsItem{
			{HSCode: "61091000", Value: 50.0, Quantity: 1},
		},
	}
	items, _ := buildPostNordItems(c)
	require.Len(t, items, 1)
	assert.Equal(t, "EUR", items[0].Currency)
}

// ─── glsIncotermCode ─────────────────────────────────────────────────────────

func TestGLSIncotermCode(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 10, glsIncotermCode("DDP"))
	assert.Equal(t, 20, glsIncotermCode("DAP"))
	assert.Equal(t, 20, glsIncotermCode("FOB"))  // unmapped → DAP
	assert.Equal(t, 20, glsIncotermCode("EXW"))  // unmapped → DAP
	assert.Equal(t, 20, glsIncotermCode(""))
}

// ─── dhlIncoterms ────────────────────────────────────────────────────────────

func TestDHLIncoterms(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "DDP", dhlIncoterms("DDP"))
	assert.Equal(t, "DAP", dhlIncoterms("DAP"))
	assert.Equal(t, "DAP", dhlIncoterms("FOB"))
	assert.Equal(t, "DAP", dhlIncoterms(""))
}

// ─── postNordReasonForExportation ────────────────────────────────────────────

func TestPostNordReasonForExportation(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 4000, postNordReasonForExportation("B2B"))
	assert.Equal(t, 4000, postNordReasonForExportation("B2C"))
	assert.Equal(t, 1000, postNordReasonForExportation(""))
	assert.Equal(t, 1000, postNordReasonForExportation("GIFT"))
}

// ─── DHLAdapter.SubmitCustoms (HTTP round-trip) ───────────────────────────────

func TestDHLAdapter_SubmitCustoms_Success(t *testing.T) {
	t.Parallel()

	// Token server.
	tokenCalls := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// Customs endpoint.
	customsCalls := 0
	customsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customsCalls++
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/ccc/send-cCustoms", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer customsServer.Close()

	log, _ := zap.NewDevelopment()
	a := NewDHLAdapter("client-id", "client-secret", "cust-001", "", log)
	a.BookingBaseURL = customsServer.URL
	// Override token fetch to use token server.
	a.BookingBaseURL = tokenServer.URL
	a.BookingBaseURL = customsServer.URL

	// Pre-populate token so we can test the customs call directly.
	a.tokenCache.accessToken = "test-token"
	a.tokenCache.expiresAt = time.Now().Add(time.Hour) // far future

	resp, err := a.SubmitCustoms(context.Background(), testCustomsRequest())
	require.NoError(t, err)
	assert.Equal(t, "dhl", resp.Carrier)
	assert.Equal(t, "submitted", resp.Status)
	assert.Equal(t, 1, customsCalls)
}

func TestDHLAdapter_SubmitCustoms_CarrierError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid parcelIdentifier"}`))
	}))
	defer server.Close()

	log, _ := zap.NewDevelopment()
	a := NewDHLAdapter("id", "secret", "cust", "", log)
	a.BookingBaseURL = server.URL
	a.tokenCache.accessToken = "tok"
	a.tokenCache.expiresAt = time.Now().Add(time.Hour)

	_, err := a.SubmitCustoms(context.Background(), testCustomsRequest())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// ─── PostNordAdapter.SubmitCustoms ────────────────────────────────────────────

func TestPostNordAdapter_SubmitCustoms_MissingEDIItemID(t *testing.T) {
	t.Parallel()

	log, _ := zap.NewDevelopment()
	a := NewPostNordAdapter("apikey", "cust", 0, log)

	req := testCustomsRequest()
	req.EDIItemID = ""

	_, err := a.SubmitCustoms(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EDIItemID is required")
}

func TestPostNordAdapter_SubmitCustoms_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/shipment/v3/customs/declaration", r.URL.Path)
		assert.Equal(t, "test-api-key", r.URL.Query().Get("apikey"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	log, _ := zap.NewDevelopment()
	a := NewPostNordAdapter("test-api-key", "cust", 0, log)
	a.BaseURL = server.URL

	resp, err := a.SubmitCustoms(context.Background(), testCustomsRequest())
	require.NoError(t, err)
	assert.Equal(t, "postnord", resp.Carrier)
	assert.Equal(t, "submitted", resp.Status)
}

// ─── GLSAdapter.SubmitCustoms ─────────────────────────────────────────────────

func TestGLSAdapter_SubmitCustoms_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "customs-consignments")
		assert.Equal(t, "Bearer gls-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"consignmentId": "GLS-CUSTOMS-001",
			"status":        "created",
		})
	}))
	defer server.Close()

	log, _ := zap.NewDevelopment()
	a := NewGLSAdapter("client", "secret", "contact", log)
	// Hijack the customs URL to point at our test server and pre-populate token.
	a.tokenCache.accessToken = "gls-token"
	a.tokenCache.expiresAt = time.Now().Add(time.Hour)

	// We can't override glsCustomsBaseURL since it's a const.
	// This test validates the payload construction path only by using
	// a mock server and overriding the HTTPClient transport.
	a.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			server.Config.Handler.ServeHTTP(rec, req)
			return rec.Result(), nil
		}),
	}

	resp, err := a.SubmitCustoms(context.Background(), testCustomsRequest())
	require.NoError(t, err)
	assert.Equal(t, "gls", resp.Carrier)
	assert.Equal(t, "GLS-CUSTOMS-001", resp.DeclarationID)
}

// roundTripFunc allows injecting a custom RoundTripper for testing.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
