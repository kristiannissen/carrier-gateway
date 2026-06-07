// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/handler_test.go.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// newTestConfig returns a handler Config wired with mock adapters.
// The registry is built directly from the mock adapter map so no env var
// manipulation is needed and tests can safely call t.Parallel().
func newTestConfig(t *testing.T) *Config {
	t.Helper()
	log := zaptest.NewLogger(t)
	// Build a registry from the raw mock adapter map rather than going
	// through InitAdapters which reads MOCK_MODE from the environment.
	adapters := map[string]adapter.CarrierAdapter{
		"postnord": &adapter.MockPostNordAdapter{},
		"bring":    &adapter.MockBringAdapter{},
		"gls":      &adapter.MockGLSAdapter{},
		"dao":      &adapter.MockDAOAdapter{},
		"posti":    &adapter.MockPostiAdapter{},
		"inpost":   &adapter.MockInPostAdapter{},
	}
	return &Config{
		Registry: adapter.NewRegistryFromMap(adapters),
		Log:      log,
		MockMode: true,
	}
}

// minimalBookingBody returns a valid booking request JSON body for the given carrier.
func minimalBookingBody(carrier string) []byte {
	req := map[string]interface{}{
		"carrier": carrier,
		"shipment": map[string]interface{}{
			"sender": map[string]interface{}{
				"name":       "Unisport Group",
				"street":     "Industrivej",
				"houseNumber": "10",
				"city":       "Copenhagen",
				"postalCode": "2300",
				"country":    "DK",
			},
			"receiver": map[string]interface{}{
				"name":       "Test Receiver",
				"street":     "Main Street",
				"houseNumber": "1",
				"city":       "Stockholm",
				"postalCode": "11122",
				"country":    "SE",
			},
			"totalWeight": 1.0,
			"colli": []map[string]interface{}{
				{
					"id":     "box-1",
					"weight": 1.0,
					"dimensions": map[string]interface{}{
						"length": 10, "width": 10, "height": 10,
					},
					"items": []map[string]interface{}{
						{"description": "item", "weight": 0.5, "quantity": 1},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(req)
	return b
}

// =========================================================================
// POST /api/bookings
// =========================================================================

func TestBookShipment_ValidRequest(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		bytes.NewReader(minimalBookingBody("postnord")))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.BookingResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "postnord", resp.Carrier)
	assert.NotEmpty(t, resp.TrackingNumber)
}

func TestBookShipment_UnsupportedCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		bytes.NewReader(minimalBookingBody("fedex")))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "unsupported carrier", resp.Error)
}

func TestBookShipment_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodGet, "/api/bookings", nil)
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestBookShipment_InvalidJSON(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader("not-json"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBookShipment_MissingCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	body, _ := json.Marshal(map[string]interface{}{
		"shipment": map[string]interface{}{
			"totalWeight": 1.0,
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "validation failed", resp.Error)
}

func TestBookShipment_UnsupportedContentType(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader("<xml/>"))
	r.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()

	cfg.BookShipment(w, r)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestBookShipment_AllCarriers(t *testing.T) {
	t.Parallel()
	for _, carrier := range []string{"postnord", "bring", "gls", "dao", "posti", "inpost"} {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			r := httptest.NewRequest(http.MethodPost, "/api/bookings",
				bytes.NewReader(minimalBookingBody(carrier)))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			cfg.BookShipment(w, r)

			assert.Equal(t, http.StatusOK, w.Code,
				"expected 200 for carrier %s", carrier)
			var resp adapter.BookingResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, carrier, resp.Carrier)
		})
	}
}

// =========================================================================
// GET /api/trackings/{trackingNumber}
// =========================================================================

func TestGetTracking_ValidRequest(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)

	r := httptest.NewRequest(http.MethodGet, "/api/trackings/PN123456789DK?carrier=postnord", nil)
	w := httptest.NewRecorder()

	// Wire through mux so path variables are populated.
	router := mux.NewRouter()
	router.HandleFunc("/api/trackings/{trackingNumber}", cfg.GetTracking).Methods("GET")
	router.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.TrackingResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "postnord", resp.Carrier)
}

func TestGetTracking_DefaultsToPostNord(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodGet, "/api/trackings/PN123456789DK", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/api/trackings/{trackingNumber}", cfg.GetTracking).Methods("GET")
	router.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetTracking_UnsupportedCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodGet, "/api/trackings/FX123?carrier=fedex", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/api/trackings/{trackingNumber}", cfg.GetTracking).Methods("GET")
	router.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTracking_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/trackings/PN123", nil)
	w := httptest.NewRecorder()

	router := mux.NewRouter()
	router.HandleFunc("/api/trackings/{trackingNumber}", cfg.GetTracking)
	router.ServeHTTP(w, r)

	cfg.GetTracking(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// =========================================================================
// GET /api/health
// =========================================================================

func TestHealthCheck_ReturnsOK(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	cfg.HealthCheck(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp HealthResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp.Status)
	assert.NotEmpty(t, resp.Uptime)
	assert.True(t, resp.MockMode)
	assert.NotEmpty(t, resp.Carriers)
}

func TestHealthCheck_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	w := httptest.NewRecorder()

	cfg.HealthCheck(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHealthCheck_CarriersReflectMockMode(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	cfg.HealthCheck(w, r)

	var resp HealthResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	for carrier, mode := range resp.Carriers {
		if carrier == "dao" {
			// DAO is always shown as beta regardless of mock mode.
			assert.Equal(t, "beta", mode,
				"carrier dao should always be beta")
		} else {
			assert.Equal(t, "mock", mode,
				"carrier %s should be mock when MockMode is true", carrier)
		}
	}
}

// =========================================================================
// writeError
// =========================================================================

func TestWriteError_ShapeAndStatusCode(t *testing.T) {
	t.Parallel()

	cfg := &Config{Log: zap.NewNop()}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	cfg.writeError(w, r, http.StatusBadRequest, "something went wrong", "detail here")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "something went wrong", resp.Error)
	assert.Equal(t, "detail here", resp.Details)
}

func TestWriteError_NoDetails(t *testing.T) {
	t.Parallel()

	cfg := &Config{Log: zap.NewNop()}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	cfg.writeError(w, r, http.StatusNotFound, "not found", "")

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "not found", resp.Error)
	assert.Empty(t, resp.Details)
}

// =========================================================================
// selectAdapter
// =========================================================================

func TestSelectAdapter_KnownCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	a, err := cfg.selectAdapter("postnord")
	require.NoError(t, err)
	assert.NotNil(t, a)
}

func TestSelectAdapter_UnknownCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	_, err := cfg.selectAdapter("fedex")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fedex")
}

// =========================================================================
// Context cancellation
// =========================================================================

func TestBookShipment_CancelledContext(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		bytes.NewReader(minimalBookingBody("postnord"))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Mock adapters don't check context cancellation, so we just verify
	// the handler doesn't panic on a cancelled context.
	assert.NotPanics(t, func() {
		cfg.BookShipment(w, r)
	})
}
