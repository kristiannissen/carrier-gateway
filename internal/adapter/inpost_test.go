// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/inpost_test.go.
package adapter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// Mock adapter tests — exercise MockInPostAdapter directly
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
// Real adapter — OAuth token flow
// =========================================================================

func TestInPostAdapter_BearerToken_FetchedAndCached(t *testing.T) {
	t.Parallel()

	calls := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		assert.Equal(t, "test-client", r.Form.Get("client_id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-abc","expires_in":599}`))
	}))
	t.Cleanup(tokenSrv.Close)

	a := &InPostAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		OrgID:        "test-org",
		BaseURL:      tokenSrv.URL,
		AuthURL:      tokenSrv.URL + "/oauth2/token",
		HTTPClient:   tokenSrv.Client(),
	}

	tok1, err := a.bearerToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", tok1)
	assert.Equal(t, 1, calls, "first call fetches a token")

	tok2, err := a.bearerToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", tok2)
	assert.Equal(t, 1, calls, "second call uses the cache")
}

func TestInPostAdapter_BearerToken_RefreshedWhenExpired(t *testing.T) {
	t.Parallel()

	calls := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-tok","expires_in":599}`))
	}))
	t.Cleanup(tokenSrv.Close)

	a := &InPostAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		OrgID:        "test-org",
		BaseURL:      tokenSrv.URL,
		AuthURL:      tokenSrv.URL + "/oauth2/token",
		HTTPClient:   tokenSrv.Client(),
	}
	// Seed an already-expired token.
	a.tokenCache.accessToken = "old-tok"
	a.tokenCache.expiresAt = time.Now().Add(-1 * time.Minute)

	tok, err := a.bearerToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fresh-tok", tok)
	assert.Equal(t, 1, calls, "expired cache triggers one fetch")
}

// =========================================================================
// Real adapter — BookShipment payload shape
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

	// No top-level "shipment" wrapper in the v2 API.
	_, hasShipmentWrapper := payload["shipment"]
	assert.False(t, hasShipmentWrapper, "v2 API must not have a top-level 'shipment' wrapper")

	// Sender
	sender := inpostRequireNested(t, payload, "sender")
	assert.Equal(t, "Sender Shop", sender["companyName"])
	assert.Equal(t, "Sender", sender["firstName"])
	assert.Equal(t, "Shop", sender["lastName"])
	assert.Equal(t, "+4812345678", sender["phone"])
	assert.Equal(t, "sender@example.com", sender["email"])

	// Recipient — must be "recipient", not "receiver"
	_, hasReceiver := payload["receiver"]
	assert.False(t, hasReceiver, "InPost v2 uses 'recipient', not 'receiver'")
	recipient := inpostRequireNested(t, payload, "recipient")
	assert.Equal(t, "John Kowalski", recipient["companyName"])
	assert.Equal(t, "+48987654321", recipient["phone"])

	// Origin
	origin := inpostRequireNested(t, payload, "origin")
	assert.Equal(t, "PL", origin["countryCode"])
	assert.Equal(t, "APM", origin["shippingMethod"])

	// Destination — home delivery (no ServicePointID)
	dest := inpostRequireNested(t, payload, "destination")
	assert.Equal(t, "PL", dest["countryCode"])
	assert.Equal(t, "Krakow", dest["city"])
	assert.Equal(t, "30-001", dest["postalCode"])
	_, hasPointID := dest["pointId"]
	assert.False(t, hasPointID, "home delivery must not have pointId")

	// Parcels — new wire format
	parcels := inpostRequireArray(t, payload, "parcels", 1)
	parcel := parcels[0].(map[string]any)
	assert.Equal(t, "STANDARD", parcel["type"])

	dims := inpostRequireNested(t, parcel, "dimensions")
	assert.Equal(t, "10", dims["length"])
	assert.Equal(t, "10", dims["width"])
	assert.Equal(t, "10", dims["height"])
	assert.Equal(t, "CM", dims["unit"])

	weight := inpostRequireNested(t, parcel, "weight")
	assert.Equal(t, "2000", weight["amount"]) // 2.0 kg → 2000 g
	assert.Equal(t, "G", weight["unit"])
}

func TestInPostAdapter_BookShipment_APMDestination(t *testing.T) {
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

	dest := inpostRequireNested(t, *captured, "destination")
	assert.Equal(t, "WAR001", dest["pointId"])
	assert.Equal(t, "PL", dest["countryCode"])
	_, hasCity := dest["city"]
	assert.False(t, hasCity, "locker destination must not have city")
}

func TestInPostAdapter_BookShipment_NoLockerByDefault(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	_, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	require.NoError(t, err)

	dest := inpostRequireNested(t, *captured, "destination")
	_, hasPointID := dest["pointId"]
	assert.False(t, hasPointID, "home delivery must not include pointId")
}

func TestInPostAdapter_BookShipment_IdempotencyHeader(t *testing.T) {
	t.Parallel()

	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Deduplication-Id")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(inpostMockBookingResponse()))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededInPostAdapter(srv)
	req := inpostMinimalRequest()
	req.IdempotencyKey = "my-dedup-key"

	_, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "my-dedup-key", capturedHeader)
}

func TestInPostAdapter_BookShipment_IdempotencyInBody(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.IdempotencyKey = "INPOST-ORDER-12345"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	refs := inpostRequireNested(t, *captured, "references")
	custom := inpostRequireNested(t, refs, "custom")
	assert.Equal(t, "INPOST-ORDER-12345", custom["invoiceNumber"])
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

	parcels := inpostRequireArray(t, *captured, "parcels", 2)

	p0 := parcels[0].(map[string]any)
	w0 := inpostRequireNested(t, p0, "weight")
	assert.Equal(t, "2000", w0["amount"])

	p1 := parcels[1].(map[string]any)
	w1 := inpostRequireNested(t, p1, "weight")
	assert.Equal(t, "3000", w1["amount"])
}

func TestInPostAdapter_BookShipment_GBSubdivisionCode(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.Shipment.Sender = Address{
		Name:       "GB Sender",
		Street:     "1 High St",
		City:       "London",
		PostalCode: "SW1A 1AA",
		Country:    "GB",
		State:      "GB-ENG",
	}

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	origin := inpostRequireNested(t, *captured, "origin")
	assert.Equal(t, "GB", origin["countryCode"])
	assert.Equal(t, "GB-ENG", origin["subdivisionCode"])
}

func TestInPostAdapter_BookShipment_ResponseMapped(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	response, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	require.NoError(t, err)

	assert.Equal(t, "INPOST123456789PL", response.TrackingNumber)
	assert.Equal(t, "inpost", response.Carrier)
	assert.Equal(t, "booked", response.Status)
}

func TestInPostAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusBadRequest, `{"error":"invalid request"}`)

	_, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestInPostAdapter_BookShipment_ValidationError(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusUnprocessableEntity, `{"errors":["field required"]}`)

	_, err := adapter.BookShipment(t.Context(), inpostMinimalRequest())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "422")
}

func TestInPostAdapter_BookShipment_DropOffCode(t *testing.T) {
	t.Parallel()

	adapter, captured := newInPostTestServer(t, http.StatusCreated, inpostMockBookingResponse())

	req := inpostMinimalRequest()
	req.Shipment.ReturnFunctionality = "labelless"

	_, err := adapter.BookShipment(t.Context(), req)
	require.NoError(t, err)

	v, ok := (*captured)["enableDropOffCode"]
	require.True(t, ok, "enableDropOffCode must be present")
	assert.Equal(t, true, v)
}

// =========================================================================
// Real adapter — FetchLabel
// =========================================================================

func TestInPostAdapter_FetchLabel_AcceptHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		format LabelFormat
		want   string
	}{
		{LabelFormatPDF, "application/pdf;format=A6"},
		{LabelFormatZPL, "text/zpl;dpi=203"},
		{LabelFormatZPLGK, "text/zpl;dpi=300"},
		{LabelFormatEPL, "text/epl2;dpi=203"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.format), func(t *testing.T) {
			t.Parallel()

			var capturedAccept string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedAccept = r.Header.Get("Accept")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("fake-label-bytes"))
			}))
			t.Cleanup(srv.Close)

			a := newPreseededInPostAdapter(srv)
			_, err := a.FetchLabel(t.Context(), LabelRequest{
				TrackingNumber: "INPOST123456789PL",
				Format:         tc.format,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, capturedAccept)
		})
	}
}

func TestInPostAdapter_FetchLabel_EndpointPath(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-label-bytes"))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededInPostAdapter(srv)
	_, err := a.FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: "INPOST123456789PL",
		Format:         LabelFormatPDF,
	})
	require.NoError(t, err)
	assert.Equal(t, "/shipping/v2/organizations/test-org/shipments/INPOST123456789PL/label", capturedPath)
}

func TestInPostAdapter_FetchLabel_MissingTrackingNumber(t *testing.T) {
	t.Parallel()

	a := &InPostAdapter{OrgID: "test-org"}
	_, err := a.FetchLabel(t.Context(), LabelRequest{Format: LabelFormatPDF})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tracking number")
}

// =========================================================================
// Real adapter — TrackShipment
// =========================================================================

func TestInPostAdapter_TrackShipment_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedPath, capturedQuery, capturedEventVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query().Get("trackingNumbers")
		capturedEventVersion = r.Header.Get("x-inpost-event-version")
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(inpostMockTrackingResponse("INPOST123456789PL")))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededInPostAdapter(srv)
	resp, err := a.TrackShipment(t.Context(), "INPOST123456789PL")
	require.NoError(t, err)

	assert.Equal(t, "/tracking/v1/parcels", capturedPath)
	assert.Equal(t, "INPOST123456789PL", capturedQuery)
	assert.Equal(t, "V1", capturedEventVersion)
	assert.Equal(t, "INPOST123456789PL", resp.TrackingNumber)
	assert.Equal(t, "inpost", resp.Carrier)
}

func TestInPostAdapter_TrackShipment_EventsMapped(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusOK, inpostMockTrackingResponse("TRK001"))

	resp, err := adapter.TrackShipment(t.Context(), "TRK001")
	require.NoError(t, err)

	require.Len(t, resp.Events, 1)
	assert.Equal(t, "LMD.1002", resp.Events[0].Status)
	assert.Equal(t, "Warsaw, PL", resp.Events[0].Location)
	assert.NotEmpty(t, resp.Events[0].Timestamp)
}

func TestInPostAdapter_TrackShipment_NotFound(t *testing.T) {
	t.Parallel()

	adapter, _ := newInPostTestServer(t, http.StatusOK, `{"parcels":[]}`)

	_, err := adapter.TrackShipment(t.Context(), "MISSING")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInPostAdapter_TrackShipment_MissingTrackingNumber(t *testing.T) {
	t.Parallel()

	a := &InPostAdapter{OrgID: "test-org"}
	_, err := a.TrackShipment(t.Context(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tracking number")
}

// =========================================================================
// Helpers
// =========================================================================

// newInPostTestServer creates a mock HTTP server and returns an InPostAdapter
// pre-seeded with a valid token so the OAuth flow is skipped in unit tests.
// The captured request body is decoded into the returned pointer.
func newInPostTestServer(t *testing.T, statusCode int, body string) (*InPostAdapter, *map[string]any) {
	t.Helper()

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		// Not all endpoints send a body — best-effort decode.
		_ = json.Unmarshal(raw, &captured)
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return newPreseededInPostAdapter(srv), &captured
}

// newPreseededInPostAdapter builds an InPostAdapter pointing at srv with a
// pre-seeded, non-expired token so unit tests bypass the OAuth flow entirely.
func newPreseededInPostAdapter(srv *httptest.Server) *InPostAdapter {
	a := &InPostAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		OrgID:        "test-org",
		BaseURL:      srv.URL,
		AuthURL:      srv.URL + "/oauth2/token",
		HTTPClient:   srv.Client(),
	}
	a.tokenCache.accessToken = "test-token"
	a.tokenCache.expiresAt = time.Now().Add(time.Hour)
	return a
}

func inpostMockBookingResponse() string {
	return `{"trackingNumber":"INPOST123456789PL","parcels":[{"parcelNumbers":[{"carrier":"inPost","id":"parcelNumber","value":"INPOST123456789PL"}]}],"routing":{"deliveryArea":"001454","deliveryDepotNumber":"0845"}}`
}

func inpostMockTrackingResponse(trackingNumber string) string {
	return `{"parcels":[{"trackingNumber":"` + trackingNumber + `","status":null,"events":[{"eventTimestamp":"2024-09-23T13:33:58.031+00:00","eventCode":"LMD.1002","status":null,"location":{"address":"Uxbridge Road","city":"Warsaw","country":"PL","name":"WAR001"}}]}]}`
}

func inpostRequireNested(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	require.True(t, ok, "missing or non-object key %q in %v", key, parent)
	return v
}

func inpostRequireArray(t *testing.T, parent map[string]any, key string, wantLen int) []any {
	t.Helper()
	v, ok := parent[key].([]any)
	require.True(t, ok, "missing or non-array key %q", key)
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
		Name:       "Sender Shop",
		Street:     "Sender Street",
		City:       "Warsaw",
		PostalCode: "00-001",
		Country:    "PL",
	}
}

func inpostTestReceiver() Address {
	return Address{
		Name:       "John Kowalski",
		Street:     "Receiver Avenue",
		City:       "Krakow",
		PostalCode: "30-001",
		Country:    "PL",
	}
}

func inpostTestColli(id string, weightKg float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weightKg,
		Dimensions: Dimensions{Length: 10, Width: 10, Height: 10},
	}
}
