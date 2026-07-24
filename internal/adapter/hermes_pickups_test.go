// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/hermes_pickups_test.go.
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
	"go.uber.org/zap"
)

// newPreseededHermesAdapter returns a HermesAdapter pointed at srv with a
// pre-seeded bearer token, so tests exercise the pickup endpoints without
// going through the OAuth2 client-credentials flow.
func newPreseededHermesAdapter(srv *httptest.Server) *HermesAdapter {
	a := &HermesAdapter{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		OrderBaseURL: srv.URL,
		InfoBaseURL:  srv.URL,
		AuthURL:      srv.URL + "/oauth2/access_token",
		HTTPClient:   srv.Client(),
		log:          zap.NewNop(),
	}
	a.tokenCache.accessToken = "test-token"
	a.tokenCache.expiresAt = time.Now().Add(time.Hour)
	return a
}

func hermesRequireNested(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	require.True(t, ok, "missing or non-object key %q in %v", key, parent)
	return v
}

// =========================================================================
// Real adapter — BookPickup (ManifestAdapter)
// =========================================================================

func TestHermesAdapter_BookPickup_HappyPath(t *testing.T) {
	t.Parallel()

	var capturedPath, capturedMethod string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"pickupOrderID":"01234567890"}`))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)
	resp, err := a.BookPickup(t.Context(), PickupRequest{
		Carrier: "hermes",
		Pickup: PickupWindow{
			Date:      "2026-08-01",
			ReadyTime: "10:00",
			CloseTime: "13:00",
		},
		Contact: PickupContact{Name: "Max Mustermann", Phone: "+4940537550"},
		Address: PickupAddress{
			Street:      "Essener Bogen",
			HouseNumber: "1",
			PostalCode:  "22419",
			City:        "Hamburg",
			Country:     "DE",
		},
		EstimatedParcels: 3,
	})
	require.NoError(t, err)

	assert.Equal(t, "/pickuporders", capturedPath)
	assert.Equal(t, http.MethodPost, capturedMethod)

	assert.Equal(t, "01234567890", resp.ConfirmationNumber)
	assert.Equal(t, "hermes", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.Equal(t, "2026-08-01", resp.Date)

	assert.Equal(t, "2026-08-01", capturedBody["pickupDate"])
	assert.Equal(t, "BETWEEN_10_AND_13", capturedBody["pickupTimeSlot"])
	assert.Equal(t, "+4940537550", capturedBody["phone"])

	addr := hermesRequireNested(t, capturedBody, "pickupAddress")
	assert.Equal(t, "Essener Bogen", addr["street"])
	assert.Equal(t, "DE", addr["countryCode"])

	name := hermesRequireNested(t, capturedBody, "pickupName")
	assert.Equal(t, "Max", name["firstname"])
	assert.Equal(t, "Mustermann", name["lastname"])

	parcelCount := hermesRequireNested(t, capturedBody, "parcelCount")
	assert.InDelta(t, 3.0, parcelCount["pickupParcelCountM"], 0)
}

func TestHermesAdapter_BookPickup_MissingDate(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	_, err := a.BookPickup(t.Context(), PickupRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pickup date is required")
}

func TestHermesAdapter_BookPickup_NoAddress_OmitsAddressBlock(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"pickupOrderID":"01234567890"}`))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)
	_, err := a.BookPickup(t.Context(), PickupRequest{
		Pickup: PickupWindow{Date: "2026-08-01"},
	})
	require.NoError(t, err)

	_, hasAddr := capturedBody["pickupAddress"]
	_, hasName := capturedBody["pickupName"]
	assert.False(t, hasAddr, "pickupAddress should be omitted when no address is supplied")
	assert.False(t, hasName, "pickupName should be omitted when no address is supplied")
}

func TestHermesAdapter_UpdatePickup_NotSupported(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	_, err := a.UpdatePickup(t.Context(), "01234567890", PickupRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestHermesAdapter_CancelPickup_RequestShape(t *testing.T) {
	t.Parallel()

	var capturedPath, capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"pickupOrderID":"01234567890"}`))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)
	err := a.CancelPickup(t.Context(), "hermes", "01234567890")
	require.NoError(t, err)

	assert.Equal(t, "/pickuporders/01234567890", capturedPath)
	assert.Equal(t, http.MethodDelete, capturedMethod)
}

func TestHermesAdapter_CancelPickup_EmptyConfirmationNumber(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	err := a.CancelPickup(t.Context(), "hermes", "")
	require.Error(t, err)
}

func TestHermesAdapter_CloseManifest_NotSupported(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	_, err := a.CloseManifest(t.Context(), ManifestRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestHermesAdapter_GetPickupAvailability_NotSupported(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	_, err := a.GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestHermesAdapter_GetCutoffTime_NotSupported(t *testing.T) {
	t.Parallel()

	a := &HermesAdapter{}
	_, err := a.GetCutoffTime(t.Context(), "22419", "DE")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

// =========================================================================
// Real adapter — PickupQuerier
// =========================================================================

const hermesMockPickupOrdersResponse = `{
	"listOfPickupOrders": [
		{"pickupOrderID":"01234567890","shipmentID":"H0000000001","actualOrderState":"CREATED","pickupDate":"2026-08-01","pickupTimeSlot":"BETWEEN_10_AND_13","orderCreationDate":"2026-07-20"},
		{"pickupOrderID":"01234567891","shipmentID":"H0000000002","actualOrderState":"ARCHIVED","pickupDate":"2026-08-02","pickupTimeSlot":"BETWEEN_14_AND_17","orderCreationDate":"2026-07-21"}
	]
}`

func TestHermesAdapter_GetPickupByID_Found(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pickuporders", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(hermesMockPickupOrdersResponse))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)
	info, err := a.GetPickupByID(t.Context(), "01234567891")
	require.NoError(t, err)

	assert.Equal(t, "01234567891", info.ID)
	assert.Equal(t, "01234567891", info.ConfirmationNumber)
	assert.Equal(t, "hermes", info.Carrier)
	assert.Equal(t, "ARCHIVED", info.Status)
	assert.Equal(t, "BETWEEN_14_AND_17", info.ReadyTime)
	assert.Equal(t, "2026-07-21", info.CreatedAt)
}

func TestHermesAdapter_GetPickupByID_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(hermesMockPickupOrdersResponse))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)
	_, err := a.GetPickupByID(t.Context(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no pickup order found")
}

func TestHermesAdapter_ListPickups_PagesClientSide(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The HSI API takes no query parameters — assert none are forwarded.
		assert.Empty(t, r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(hermesMockPickupOrdersResponse))
	}))
	t.Cleanup(srv.Close)

	a := newPreseededHermesAdapter(srv)

	t.Run("first page, size 1", func(t *testing.T) {
		t.Parallel()
		list, err := a.ListPickups(t.Context(), ListPickupsRequest{Page: 0, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, 1, list.Count)
		assert.Equal(t, 2, list.TotalPages)
		require.Len(t, list.Items, 1)
		assert.Equal(t, "01234567890", list.Items[0].ID)
	})

	t.Run("second page, size 1", func(t *testing.T) {
		t.Parallel()
		list, err := a.ListPickups(t.Context(), ListPickupsRequest{Page: 1, Size: 1})
		require.NoError(t, err)
		require.Len(t, list.Items, 1)
		assert.Equal(t, "01234567891", list.Items[0].ID)
	})

	t.Run("default size returns everything", func(t *testing.T) {
		t.Parallel()
		list, err := a.ListPickups(t.Context(), ListPickupsRequest{})
		require.NoError(t, err)
		assert.Equal(t, 2, list.Count)
		assert.Equal(t, 1, list.TotalPages)
	})
}

// =========================================================================
// hermesPickupTimeSlot / hermesPickupAddressPayload helpers
// =========================================================================

func TestHermesPickupTimeSlot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		readyTime string
		want      string
	}{
		{"", ""},
		{"09:00", "BETWEEN_10_AND_13"},
		{"11:59", "BETWEEN_10_AND_13"},
		{"12:00", "BETWEEN_12_AND_15"},
		{"13:30", "BETWEEN_12_AND_15"},
		{"14:00", "BETWEEN_14_AND_17"},
		{"17:00", "BETWEEN_14_AND_17"},
		{"bad-input", ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.readyTime, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, hermesPickupTimeSlot(c.readyTime))
		})
	}
}

func TestHermesPickupAddressPayload_EmptyStreetOmitted(t *testing.T) {
	t.Parallel()

	assert.Nil(t, hermesPickupAddressPayload(PickupAddress{}))
}

// =========================================================================
// Mock adapter tests
// =========================================================================

func TestMockHermesAdapter_BookPickup(t *testing.T) {
	t.Parallel()

	resp, err := NewMockHermesAdapter().BookPickup(t.Context(), PickupRequest{
		Pickup: PickupWindow{Date: "2026-08-01"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hermes", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.NotEmpty(t, resp.ConfirmationNumber)
}

func TestMockHermesAdapter_BookPickup_MissingDate(t *testing.T) {
	t.Parallel()

	_, err := NewMockHermesAdapter().BookPickup(t.Context(), PickupRequest{})
	require.Error(t, err)
}

func TestMockHermesAdapter_CancelPickup(t *testing.T) {
	t.Parallel()

	err := NewMockHermesAdapter().CancelPickup(t.Context(), "hermes", "01234567890")
	require.NoError(t, err)
}

func TestMockHermesAdapter_UpdatePickup_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockHermesAdapter().UpdatePickup(t.Context(), "01234567890", PickupRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestMockHermesAdapter_CloseManifest_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockHermesAdapter().CloseManifest(t.Context(), ManifestRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestMockHermesAdapter_GetPickupAvailability_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockHermesAdapter().GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

func TestMockHermesAdapter_GetPickupByID(t *testing.T) {
	t.Parallel()

	info, err := NewMockHermesAdapter().GetPickupByID(t.Context(), "01234567890")
	require.NoError(t, err)
	assert.Equal(t, "01234567890", info.ID)
	assert.Equal(t, "hermes", info.Carrier)
}

func TestMockHermesAdapter_ListPickups(t *testing.T) {
	t.Parallel()

	list, err := NewMockHermesAdapter().ListPickups(t.Context(), ListPickupsRequest{})
	require.NoError(t, err)
	assert.Equal(t, "hermes", list.Carrier)
	assert.NotEmpty(t, list.Items)
}

func TestMockHermesAdapter_GetCutoffTime_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockHermesAdapter().GetCutoffTime(t.Context(), "22419", "DE")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotSupported)
}

// =========================================================================
// Interface satisfaction
// =========================================================================

var (
	_ ManifestAdapter = (*HermesAdapter)(nil)
	_ PickupQuerier   = (*HermesAdapter)(nil)
	_ ManifestAdapter = (*MockHermesAdapter)(nil)
	_ PickupQuerier   = (*MockHermesAdapter)(nil)
)
