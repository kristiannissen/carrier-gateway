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

func TestHTTPSender_Send_success(t *testing.T) {
	t.Parallel()

	var received Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NotEmpty(t, r.Header.Get("X-Signature"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewHTTPSender()
	payload := Payload{Event: EventBooked, TrackingNumber: "TN123", Carrier: "postnord"}

	err := sender.Send(context.Background(), srv.URL, "secret", payload)
	require.NoError(t, err)
	assert.Equal(t, EventBooked, received.Event)
	assert.Equal(t, "TN123", received.TrackingNumber)
}

func TestHTTPSender_Send_noSignatureWhenSecretEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-Signature"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := NewHTTPSender().Send(context.Background(), srv.URL, "", Payload{})
	require.NoError(t, err)
}

func TestHTTPSender_Send_non2xxReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := NewHTTPSender().Send(context.Background(), srv.URL, "", Payload{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-2xx")
}
