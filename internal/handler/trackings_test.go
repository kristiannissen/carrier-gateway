// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/trackings_test.go.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// stubTrackingAdapter is a test CarrierAdapter that returns a fixed TrackingResponse.
type stubTrackingAdapter struct {
	response *adapter.TrackingResponse
	err      error
}

func (s *stubTrackingAdapter) BookShipment(_ context.Context, _ adapter.BookingRequest) (*adapter.BookingResponse, error) {
	return nil, nil
}
func (s *stubTrackingAdapter) TrackShipment(_ context.Context, _ string) (*adapter.TrackingResponse, error) {
	return s.response, s.err
}
func (s *stubTrackingAdapter) FetchLabel(_ context.Context, _ adapter.LabelRequest) (*adapter.LabelResponse, error) {
	return nil, nil
}
func (s *stubTrackingAdapter) CancelShipment(_ context.Context, _ string) (*adapter.CancelResponse, error) {
	return nil, nil
}
func (s *stubTrackingAdapter) UpdateShipment(_ context.Context, _ adapter.UpdateRequest) (*adapter.UpdateResponse, error) {
	return nil, nil
}

func newTrackAndNotifyConfig(t *testing.T, trackResp *adapter.TrackingResponse) *Config {
	t.Helper()
	registry := adapter.NewRegistryFromMap(map[string]adapter.CarrierAdapter{
		"postnord": &stubTrackingAdapter{response: trackResp},
	})
	return &Config{
		Registry:            registry,
		Log:                 zap.NewNop(),
		NotificationService: notification.NewService(&stubSender{}, zap.NewNop()),
	}
}

func doTrackAndNotify(t *testing.T, cfg *Config, trackingNumber string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/trackings/"+trackingNumber, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"trackingNumber": trackingNumber})

	w := httptest.NewRecorder()
	cfg.TrackAndNotify(w, req)
	return w
}

func TestTrackAndNotify_statusChangedDispatchesNotification(t *testing.T) {
	t.Parallel()

	tracking := &adapter.TrackingResponse{
		ShipmentID:       "96932007555SE",
		TrackingNumber:   "96932007555SE",
		Carrier:          "postnord",
		Status:           "DELIVERED",
		NormalizedStatus: adapter.StatusDelivered,
		OriginalStatus:   "DELIVERED",
	}

	cfg := newTrackAndNotifyConfig(t, tracking)
	body := trackAndNotifyRequest{
		Carrier:        "postnord",
		PreviousStatus: "in_transit",
		Notifications: &adapter.NotificationPreferences{
			WebhookURL: "https://example.com/hook",
		},
	}

	w := doTrackAndNotify(t, cfg, "96932007555SE", body)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp adapter.TrackingResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.NotificationsSent, 1)
	assert.Empty(t, resp.NotificationsFailed)
	assert.Equal(t, "96932007555SE", resp.ShipmentID)
}

func TestTrackAndNotify_sameStatusNoDispatch(t *testing.T) {
	t.Parallel()

	tracking := &adapter.TrackingResponse{
		TrackingNumber:   "TN123",
		Carrier:          "postnord",
		Status:           "IN_TRANSPORT",
		NormalizedStatus: adapter.StatusInTransit,
		OriginalStatus:   "IN_TRANSPORT",
	}

	cfg := newTrackAndNotifyConfig(t, tracking)
	body := trackAndNotifyRequest{
		Carrier:        "postnord",
		PreviousStatus: "in_transit", // same as current
		Notifications: &adapter.NotificationPreferences{
			WebhookURL: "https://example.com/hook",
		},
	}

	w := doTrackAndNotify(t, cfg, "TN123", body)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp adapter.TrackingResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.NotificationsSent)
	assert.Empty(t, resp.NotificationsFailed)
}

func TestTrackAndNotify_noNotificationsPrefs(t *testing.T) {
	t.Parallel()

	tracking := &adapter.TrackingResponse{
		TrackingNumber:   "TN123",
		Carrier:          "postnord",
		NormalizedStatus: adapter.StatusDelivered,
	}

	cfg := newTrackAndNotifyConfig(t, tracking)
	body := trackAndNotifyRequest{Carrier: "postnord"}

	w := doTrackAndNotify(t, cfg, "TN123", body)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp adapter.TrackingResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.NotificationsSent)
	assert.Empty(t, resp.NotificationsFailed)
}

func TestTrackAndNotify_missingCarrier(t *testing.T) {
	t.Parallel()

	cfg := newTrackAndNotifyConfig(t, &adapter.TrackingResponse{})
	w := doTrackAndNotify(t, cfg, "TN123", trackAndNotifyRequest{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrackAndNotify_unsupportedCarrier(t *testing.T) {
	t.Parallel()

	cfg := newTrackAndNotifyConfig(t, &adapter.TrackingResponse{})
	body := trackAndNotifyRequest{Carrier: "unknown-carrier"}
	w := doTrackAndNotify(t, cfg, "TN123", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTrackAndNotify_webhookURLRequiredWhenNotificationsSet(t *testing.T) {
	t.Parallel()

	cfg := newTrackAndNotifyConfig(t, &adapter.TrackingResponse{})
	body := trackAndNotifyRequest{
		Carrier: "postnord",
		Notifications: &adapter.NotificationPreferences{
			WebhookURL: "", // empty — should fail validation
		},
	}
	w := doTrackAndNotify(t, cfg, "TN123", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
