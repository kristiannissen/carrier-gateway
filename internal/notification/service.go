// Package notification provides event-driven webhook dispatch for shipment events.
// This file is located at /internal/notification/service.go.
package notification

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// Service dispatches shipment event notifications to integrator-provided endpoints.
// It is stateless: every call to Dispatch is self-contained and returns the full
// outcome so the caller can store or act on it.
type Service struct {
	sender Sender
	log    *zap.Logger
}

// NewService returns a Service using sender for webhook delivery.
func NewService(sender Sender, log *zap.Logger) *Service {
	return &Service{sender: sender, log: log}
}

// Dispatch fires a notification for event according to prefs.
// It returns (sent, failed) slices — one record per attempted channel.
// A non-nil failed slice does not indicate a Service error; the caller
// should surface failed records to the integrator for retry decisions.
func (s *Service) Dispatch(ctx context.Context, event Event, prefs Preferences, payload Payload) (sent, failed []Record) {
	if prefs.Webhook == nil {
		return nil, nil
	}

	w := prefs.Webhook

	if !s.wantsEvent(w, event) {
		return nil, nil
	}

	payload.Event = event
	rec := Record{
		Event:     event,
		Channel:   "webhook",
		URL:       w.URL,
		Timestamp: time.Now().UTC(),
	}

	if err := s.sender.Send(ctx, w.URL, w.Secret, payload); err != nil {
		s.log.Warn("webhook dispatch failed",
			zap.String("url", w.URL),
			zap.Stringer("event", event),
			zap.Error(err),
		)
		rec.Status = "failed"
		rec.Error = err.Error()
		return nil, []Record{rec}
	}

	rec.Status = "sent"
	return []Record{rec}, nil
}

// wantsEvent reports whether the webhook should fire for event.
// An empty Events filter means all events are dispatched.
func (s *Service) wantsEvent(w *WebhookPrefs, event Event) bool {
	if len(w.Events) == 0 {
		return true
	}
	for _, e := range w.Events {
		if e == event {
			return true
		}
	}
	return false
}

// String implements fmt.Stringer so Event values log cleanly with zap.
func (e Event) String() string { return string(e) }
