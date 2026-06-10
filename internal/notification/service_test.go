// Package notification provides event-driven webhook dispatch for shipment events.
// This file is located at /internal/notification/service_test.go.
package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockSender is a test double for Sender.
type mockSender struct {
	err   error
	calls []Payload
}

func (m *mockSender) Send(_ context.Context, _, _ string, p Payload) error {
	m.calls = append(m.calls, p)
	return m.err
}

func TestService_Dispatch_sends(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	svc := NewService(ms, zap.NewNop())

	prefs := Preferences{Webhook: &WebhookPrefs{URL: "https://example.com/hook"}}
	payload := Payload{TrackingNumber: "TN123", Carrier: "postnord"}

	sent, failed := svc.Dispatch(context.Background(), EventBooked, prefs, payload)

	require.Len(t, sent, 1)
	assert.Empty(t, failed)
	assert.Equal(t, "sent", sent[0].Status)
	assert.Equal(t, EventBooked, sent[0].Event)
	assert.Equal(t, "webhook", sent[0].Channel)
	require.Len(t, ms.calls, 1)
	assert.Equal(t, EventBooked, ms.calls[0].Event)
}

func TestService_Dispatch_senderFailure(t *testing.T) {
	t.Parallel()

	ms := &mockSender{err: errors.New("connection refused")}
	svc := NewService(ms, zap.NewNop())

	prefs := Preferences{Webhook: &WebhookPrefs{URL: "https://example.com/hook"}}

	sent, failed := svc.Dispatch(context.Background(), EventDelivered, prefs, Payload{})

	assert.Empty(t, sent)
	require.Len(t, failed, 1)
	assert.Equal(t, "failed", failed[0].Status)
	assert.Contains(t, failed[0].Error, "connection refused")
}

func TestService_Dispatch_nilWebhook(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockSender{}, zap.NewNop())
	sent, failed := svc.Dispatch(context.Background(), EventBooked, Preferences{}, Payload{})

	assert.Empty(t, sent)
	assert.Empty(t, failed)
}

func TestService_Dispatch_eventFilter_matches(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	svc := NewService(ms, zap.NewNop())
	prefs := Preferences{Webhook: &WebhookPrefs{
		URL:    "https://example.com/hook",
		Events: []Event{EventDelivered, EventDelayed},
	}}

	sent, _ := svc.Dispatch(context.Background(), EventDelivered, prefs, Payload{})
	require.Len(t, sent, 1)
}

func TestService_Dispatch_eventFilter_noMatch(t *testing.T) {
	t.Parallel()

	ms := &mockSender{}
	svc := NewService(ms, zap.NewNop())
	prefs := Preferences{Webhook: &WebhookPrefs{
		URL:    "https://example.com/hook",
		Events: []Event{EventDelivered},
	}}

	sent, failed := svc.Dispatch(context.Background(), EventBooked, prefs, Payload{})
	assert.Empty(t, sent)
	assert.Empty(t, failed)
	assert.Empty(t, ms.calls, "sender must not be called when event is filtered out")
}
