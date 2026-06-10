// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/notifications.go.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// notifyRequest is the body accepted by POST /api/notifications.
type notifyRequest struct {
	// TrackingNumber identifies the shipment.
	TrackingNumber string `json:"trackingNumber"`
	// Carrier is the carrier key (e.g. "postnord", "bring").
	Carrier string `json:"carrier"`
	// Event is the shipment lifecycle event to notify about.
	Event notification.Event `json:"event"`
	// EstimatedDelivery is the carrier-provided ETA, forwarded to the integrator.
	EstimatedDelivery string `json:"estimatedDelivery,omitempty"`
	// DelayReason is set when Event is "delayed".
	DelayReason string `json:"delayReason,omitempty"`
	// Notifications holds the integrator-supplied webhook configuration.
	Notifications notification.Preferences `json:"notifications"`
}

// notifyResponse is returned by POST /api/notifications.
type notifyResponse struct {
	NotificationsSent   []notification.Record `json:"notificationsSent"`
	NotificationsFailed []notification.Record `json:"notificationsFailed"`
}

// SendNotification handles POST /api/notifications.
// The caller provides the current shipment event and webhook preferences;
// the gateway dispatches and returns the full outcome. Nothing is stored.
func (c *Config) SendNotification(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodPost {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only POST is supported")
		return
	}

	if c.NotificationService == nil {
		c.writeError(w, r, http.StatusServiceUnavailable, "notifications not configured", "notification service is not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	var req notifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	if err := validateNotifyRequest(req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	payload := notification.Payload{
		TrackingNumber:    req.TrackingNumber,
		Carrier:           req.Carrier,
		EstimatedDelivery: req.EstimatedDelivery,
		DelayReason:       req.DelayReason,
	}

	sent, failed := c.NotificationService.Dispatch(r.Context(), req.Event, req.Notifications, payload)

	log.Info("notification dispatch complete",
		zap.String("trackingNumber", req.TrackingNumber),
		zap.Stringer("event", req.Event),
		zap.Int("sent", len(sent)),
		zap.Int("failed", len(failed)),
	)

	// Always 200 — the caller decides what to do with failed records.
	// A 5xx here would prevent the caller from receiving the failure detail.
	resp := notifyResponse{
		NotificationsSent:   sent,
		NotificationsFailed: failed,
	}
	if resp.NotificationsSent == nil {
		resp.NotificationsSent = []notification.Record{}
	}
	if resp.NotificationsFailed == nil {
		resp.NotificationsFailed = []notification.Record{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}

// validateNotifyRequest checks that all required fields are present.
func validateNotifyRequest(req notifyRequest) error {
	if req.TrackingNumber == "" {
		return fmt.Errorf("trackingNumber is required")
	}
	if req.Carrier == "" {
		return fmt.Errorf("carrier is required")
	}
	if req.Event == "" {
		return fmt.Errorf("event is required")
	}
	if req.Notifications.Webhook == nil {
		return fmt.Errorf("notifications.webhook is required")
	}
	if req.Notifications.Webhook.URL == "" {
		return fmt.Errorf("notifications.webhook.url is required")
	}
	return nil
}
