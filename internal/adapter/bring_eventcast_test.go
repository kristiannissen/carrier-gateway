// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/bring_eventcast_test.go.
package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// RegisterCustomerWebhook
// =========================================================================

func TestBringAdapter_RegisterCustomerWebhook_HappyPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/event-cast/api/v1/customer/webhooks", r.URL.Path)
		assert.Equal(t, "test-uid", r.Header.Get("X-MyBring-API-Uid"))
		assert.Equal(t, "test-key", r.Header.Get("X-MyBring-API-Key"))
		assert.Empty(t, r.Header.Get("Authorization"), "Bring must not use Authorization header")

		var body BringCustomerWebhookRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "123456789", body.CustomerNumber)
		assert.Contains(t, body.EventSet, BringEventDelivered)
		assert.Equal(t, "https://hooks.example.com/bring", body.WebhookConfiguration.WebhookURL)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(bringMockCustomerWebhookResponse()))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)

	resp, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
		CustomerNumber: "123456789",
		EventSet:       []BringEventSet{BringEventDelivered, BringEventPreNotified},
		WebhookConfiguration: BringWebhookConfig{
			WebhookURL: "https://hooks.example.com/bring",
			Headers: []BringWebhookHeader{
				{Key: "x-secret", Value: "hunter2"},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "6e5ee30a-1419-4cdf-b63d-e75fbd83720f", resp.ID)
	assert.Equal(t, "123456789", resp.CustomerNumber)
	assert.NotZero(t, resp.Expiry)
}

func TestBringAdapter_RegisterCustomerWebhook_DefaultContentType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body BringCustomerWebhookRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "application/json", body.WebhookConfiguration.ContentType)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(bringMockCustomerWebhookResponse()))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)

	_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
		CustomerNumber: "123456789",
		EventSet:       []BringEventSet{BringEventDelivered},
		WebhookConfiguration: BringWebhookConfig{
			WebhookURL: "https://hooks.example.com/bring",
			// ContentType intentionally omitted — should default to application/json
		},
	})
	require.NoError(t, err)
}

func TestBringAdapter_RegisterCustomerWebhook_ValidationErrors(t *testing.T) {
	t.Parallel()

	a := &BringAdapter{APIKey: "k", CustomerID: "u", BaseURL: "http://unused", HTTPClient: http.DefaultClient}

	t.Run("missing customer number", func(t *testing.T) {
		t.Parallel()
		_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
			EventSet:             []BringEventSet{BringEventDelivered},
			WebhookConfiguration: BringWebhookConfig{WebhookURL: "https://example.com"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "customerNumber")
	})

	t.Run("empty event set", func(t *testing.T) {
		t.Parallel()
		_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
			CustomerNumber:       "123",
			WebhookConfiguration: BringWebhookConfig{WebhookURL: "https://example.com"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "eventSet")
	})

	t.Run("missing webhook URL", func(t *testing.T) {
		t.Parallel()
		_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
			CustomerNumber:       "123",
			EventSet:             []BringEventSet{BringEventDelivered},
			WebhookConfiguration: BringWebhookConfig{},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "webhookUrl")
	})
}

func TestBringAdapter_RegisterCustomerWebhook_APIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"reason":"Invalid webhook Url","status":400}`))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)

	_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
		CustomerNumber:       "123456789",
		EventSet:             []BringEventSet{BringEventDelivered},
		WebhookConfiguration: BringWebhookConfig{WebhookURL: "https://hooks.example.com"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestBringAdapter_RegisterCustomerWebhook_ConflictError(t *testing.T) {
	t.Parallel()

	// 409 means an identical subscription already exists.
	// The adapter returns an error so the caller can handle deduplication.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"Webhook Subscription already exists.","status":409}`))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)

	_, err := a.RegisterCustomerWebhook(t.Context(), BringCustomerWebhookRequest{
		CustomerNumber:       "123456789",
		EventSet:             []BringEventSet{BringEventDelivered},
		WebhookConfiguration: BringWebhookConfig{WebhookURL: "https://hooks.example.com"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "409")
}

// =========================================================================
// DeleteCustomerWebhook
// =========================================================================

func TestBringAdapter_DeleteCustomerWebhook_HappyPath(t *testing.T) {
	t.Parallel()

	const subID = "6e5ee30a-1419-4cdf-b63d-e75fbd83720f"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/event-cast/api/v1/customer/webhooks/"+subID, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)
	require.NoError(t, a.DeleteCustomerWebhook(t.Context(), subID))
}

func TestBringAdapter_DeleteCustomerWebhook_MissingID(t *testing.T) {
	t.Parallel()

	a := &BringAdapter{APIKey: "k", CustomerID: "u", BaseURL: "http://unused", HTTPClient: http.DefaultClient}
	err := a.DeleteCustomerWebhook(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscriptionID")
}

func TestBringAdapter_DeleteCustomerWebhook_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"reason":"No Customer Subscription found","status":404}`))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)
	err := a.DeleteCustomerWebhook(t.Context(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// =========================================================================
// RenewCustomerWebhook
// =========================================================================

func TestBringAdapter_RenewCustomerWebhook_HappyPath(t *testing.T) {
	t.Parallel()

	const subID = "6e5ee30a-1419-4cdf-b63d-e75fbd83720f"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/event-cast/api/v1/customer/webhooks/renew/"+subID, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bringMockCustomerWebhookRenewResponse()))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)

	resp, err := a.RenewCustomerWebhook(t.Context(), subID)
	require.NoError(t, err)
	assert.Equal(t, subID, resp.ID)
	// Expiry must be ~1 year from now after renewal
	assert.True(t, resp.Expiry.After(time.Now().Add(364*24*time.Hour)),
		"renewed expiry should be ~1 year from now")
}

func TestBringAdapter_RenewCustomerWebhook_MissingID(t *testing.T) {
	t.Parallel()

	a := &BringAdapter{APIKey: "k", CustomerID: "u", BaseURL: "http://unused", HTTPClient: http.DefaultClient}
	_, err := a.RenewCustomerWebhook(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscriptionID")
}

func TestBringAdapter_RenewCustomerWebhook_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"reason":"No Customer Subscription found","status":404}`))
	}))
	t.Cleanup(srv.Close)

	a := bringEventCastTestAdapter(srv)
	_, err := a.RenewCustomerWebhook(t.Context(), "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// =========================================================================
// Helpers
// =========================================================================

func bringEventCastTestAdapter(srv *httptest.Server) *BringAdapter {
	return &BringAdapter{
		APIKey:         "test-key",
		CustomerID:     "test-uid",
		CustomerNumber: "123456789",
		BaseURL:        srv.URL,
		HTTPClient:     srv.Client(),
	}
}

func bringMockCustomerWebhookResponse() string {
	return `{
		"id": "6e5ee30a-1419-4cdf-b63d-e75fbd83720f",
		"customerNumber": "123456789",
		"eventSet": ["DELIVERED", "PRE_NOTIFIED"],
		"webhookConfiguration": {
			"webhookUrl": "https://hooks.example.com/bring",
			"contentType": "application/json",
			"headers": [{"key": "x-secret"}]
		},
		"created": "2024-05-22T07:42:13.86645Z",
		"expiry": "2025-05-22T07:42:13.86645Z"
	}`
}

func bringMockCustomerWebhookRenewResponse() string {
	// Expiry set to well beyond 1 year from now so the assertion holds regardless of test run time.
	expiry := time.Now().Add(366 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return `{
		"id": "6e5ee30a-1419-4cdf-b63d-e75fbd83720f",
		"customerNumber": "123456789",
		"eventSet": ["DELIVERED", "PRE_NOTIFIED"],
		"webhookConfiguration": {
			"webhookUrl": "https://hooks.example.com/bring",
			"contentType": "application/json"
		},
		"created": "2024-05-22T07:42:13.86645Z",
		"expiry": "` + expiry + `"
	}`
}
