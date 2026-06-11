// Package notification provides event-driven webhook dispatch for shipment events.
// This file is located at /internal/notification/webhook_test.go.
package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSign(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	body := []byte(`{"event":"booked"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body) //nolint:errcheck
	want := hex.EncodeToString(mac.Sum(nil))

	got := sign(body, secret)
	assert.Equal(t, want, got)
}

// tlsSender returns an HTTPSender whose client trusts the test server certificate.
// Tests must use httptest.NewTLSServer to satisfy the HTTPS enforcement check.
func tlsSender(srv *httptest.Server) *HTTPSender {
	return &HTTPSender{client: srv.Client()}
}

func TestHTTPSender_Send_success(t *testing.T) {
	t.Parallel()

	var received Payload
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NotEmpty(t, r.Header.Get("X-Signature"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := Payload{Event: EventBooked, TrackingNumber: "TN123", Carrier: "postnord"}

	err := tlsSender(srv).Send(context.Background(), srv.URL, "secret", payload)
	require.NoError(t, err)
	assert.Equal(t, EventBooked, received.Event)
	assert.Equal(t, "TN123", received.TrackingNumber)
}

func TestHTTPSender_Send_noSignatureWhenSecretEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-Signature"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := tlsSender(srv).Send(context.Background(), srv.URL, "", Payload{})
	require.NoError(t, err)
}

func TestHTTPSender_Send_non2xxReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := tlsSender(srv).Send(context.Background(), srv.URL, "", Payload{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-2xx")
}

func TestHTTPSender_Send_httpsRequired(t *testing.T) {
	t.Parallel()

	err := NewHTTPSender().Send(context.Background(), "http://example.com/hook", "", Payload{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must use https")
}

func TestHTTPSender_Send_eventTypeHeader(t *testing.T) {
	t.Parallel()

	var gotEventType string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventType = r.Header.Get("X-Event-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := Payload{Event: EventDelivered, TrackingNumber: "TN999", Carrier: "bring"}
	err := tlsSender(srv).Send(context.Background(), srv.URL, "", payload)
	require.NoError(t, err)
	assert.Equal(t, "delivered", gotEventType)
}
