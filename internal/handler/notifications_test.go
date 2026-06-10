// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/notifications_test.go.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// stubSender is a test Sender that always succeeds.
type stubSender struct{}

func (s *stubSender) Send(_ context.Context, _, _ string, _ notification.Payload) error {
	return nil
}

func newNotificationTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Log:                 zap.NewNop(),
		NotificationService: notification.NewService(&stubSender{}, zap.NewNop()),
	}
}

func TestSendNotification_success(t *testing.T) {
	t.Parallel()

	body := notifyRequest{
		TrackingNumber: "TN123",
		Carrier:        "postnord",
		Event:          notification.EventDelivered,
		Notifications: notification.Preferences{
			Webhook: &notification.WebhookPrefs{URL: "https://example.com/hook"},
		},
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newNotificationTestConfig(t).SendNotification(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp notifyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.NotificationsSent, 1)
	assert.Empty(t, resp.NotificationsFailed)
}

func TestSendNotification_missingTrackingNumber(t *testing.T) {
	t.Parallel()

	body := notifyRequest{
		Carrier: "postnord",
		Event:   notification.EventDelivered,
		Notifications: notification.Preferences{
			Webhook: &notification.WebhookPrefs{URL: "https://example.com/hook"},
		},
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", bytes.NewReader(b))
	w := httptest.NewRecorder()

	newNotificationTestConfig(t).SendNotification(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSendNotification_missingWebhookURL(t *testing.T) {
	t.Parallel()

	body := notifyRequest{
		TrackingNumber: "TN123",
		Carrier:        "postnord",
		Event:          notification.EventBooked,
		Notifications: notification.Preferences{
			Webhook: &notification.WebhookPrefs{URL: ""},
		},
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", bytes.NewReader(b))
	w := httptest.NewRecorder()

	newNotificationTestConfig(t).SendNotification(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSendNotification_serviceNotConfigured(t *testing.T) {
	t.Parallel()

	cfg := &Config{Log: zap.NewNop(), NotificationService: nil}

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()

	cfg.SendNotification(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSendNotification_wrongMethod(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	w := httptest.NewRecorder()

	newNotificationTestConfig(t).SendNotification(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
