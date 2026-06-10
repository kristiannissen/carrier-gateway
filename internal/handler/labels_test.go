// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/labels_test.go.
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// routedLabelRequest wires the request through a real mux.Router so that
// {trackingNumber} path variables are populated correctly.
func routedLabelRequest(t *testing.T, cfg *Config, url string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router := mux.NewRouter()
	router.HandleFunc("/api/labels/{trackingNumber}", cfg.GetLabel).Methods("GET")
	router.ServeHTTP(w, r)
	return w
}

// =========================================================================
// GET /api/labels/{trackingNumber}
// =========================================================================

func TestGetLabel_ValidRequest_DefaultFormat(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/PN123456789DK?carrier=postnord")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.LabelResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "postnord", resp.Carrier)
	assert.Equal(t, adapter.LabelFormatPDF, resp.Format)
	assert.NotEmpty(t, resp.Data)
	assert.Equal(t, "application/pdf", resp.MimeType)
}

func TestGetLabel_AllFormats(t *testing.T) {
	t.Parallel()
	formats := []struct {
		format   string
		mimeType string
	}{
		{"PDF", "application/pdf"},
		{"PNG", "image/png"},
		{"ZPL", "application/x-zpl"},
		{"EPL", "application/x-epl"},
		{"ZPLGK", "application/x-zpl"},
	}

	for _, tc := range formats {
		tc := tc
		t.Run(tc.format, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			w := routedLabelRequest(t, cfg,
				"/api/labels/PN123456789DK?carrier=postnord&format="+tc.format)

			assert.Equal(t, http.StatusOK, w.Code)
			var resp adapter.LabelResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, adapter.LabelFormat(tc.format), resp.Format)
			assert.Equal(t, tc.mimeType, resp.MimeType)
			assert.NotEmpty(t, resp.Data)
		})
	}
}

func TestGetLabel_FormatCaseInsensitive(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/PN123?carrier=postnord&format=pdf")

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetLabel_DefaultsToPostNord(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/PN123456789DK")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.LabelResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "postnord", resp.Carrier)
}

func TestGetLabel_InvalidFormat(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/PN123?carrier=postnord&format=TIFF")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid label format", resp.Error)
}

func TestGetLabel_UnsupportedCarrier(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/FX123?carrier=fedex")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "unsupported carrier", resp.Error)
}

func TestGetLabel_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	r := httptest.NewRequest(http.MethodPost, "/api/labels/PN123", nil)
	w := httptest.NewRecorder()
	cfg.GetLabel(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestGetLabel_DAOReturnsPDF(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/DAO123456789DK?carrier=dao&format=PDF")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.LabelResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "dao", resp.Carrier)
	assert.Equal(t, adapter.LabelFormatPDF, resp.Format)
	assert.NotEmpty(t, resp.Data)
}

func TestGetLabel_AllCarriers(t *testing.T) {
	t.Parallel()
	// All carriers including DAO now support PDF labels.
	for _, carrier := range []string{"postnord", "bring", "gls", "dao", "posti", "inpost"} {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			w := routedLabelRequest(t, cfg,
				"/api/labels/TRACK123?carrier="+carrier+"&format=PDF")

			assert.Equal(t, http.StatusOK, w.Code,
				"expected 200 for carrier %s", carrier)
			var resp adapter.LabelResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, carrier, resp.Carrier)
			assert.NotEmpty(t, resp.Data)
		})
	}
}

func TestGetLabel_TrackingNumberInResponse(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	w := routedLabelRequest(t, cfg, "/api/labels/PN987654321DK?carrier=postnord")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp adapter.LabelResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "PN987654321DK", resp.TrackingNumber)
}
