// Package handler provides the HTTP handler for tracking shipments.
// This file is located at /internal/handler/trackings.go.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// GetTracking handles GET /trackings/{trackingNumber}.
func (c *Config) GetTracking(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "tracking number is required", "")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		carrier = "postnord"
	}

	carrierAdapter, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.TrackShipment(r.Context(), trackingNumber)
	if err != nil {
		log.Error("failed to track shipment",
			zap.Error(err),
			zap.String("trackingNumber", trackingNumber),
			zap.String("carrier", carrier),
		)
		c.writeError(w, r, http.StatusInternalServerError, "tracking failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}

// trackAndNotifyRequest is the body accepted by POST /api/trackings/{trackingNumber}.
type trackAndNotifyRequest struct {
	// Carrier is the carrier key (e.g. "postnord", "bring").
	Carrier string `json:"carrier"`
	// PreviousStatus is the normalised status the caller last observed.
	// When set, a notification is only dispatched if the current status differs.
	PreviousStatus string `json:"previousStatus,omitempty"`
	// Notifications holds the integrator-supplied webhook configuration.
	// When nil, the tracking result is returned without any dispatch.
	Notifications *adapter.NotificationPreferences `json:"notifications,omitempty"`
}

// TrackAndNotify handles POST /api/trackings/{trackingNumber}.
// It tracks the shipment, detects a status change against PreviousStatus, and
// dispatches a webhook when the current status maps to a notifiable event.
// The full tracking result plus notification outcome are always returned.
func (c *Config) TrackAndNotify(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "tracking number is required", "")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		c.writeError(w, r, http.StatusRequestEntityTooLarge, "request body too large", "request body must not exceed 1 MB")
		return
	}

	var req trackAndNotifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	if err := validateTrackAndNotifyRequest(req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	carrierAdapter, err := c.selectAdapter(req.Carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	tracking, err := carrierAdapter.TrackShipment(r.Context(), trackingNumber)
	if err != nil {
		log.Error("failed to track shipment",
			zap.Error(err),
			zap.String("trackingNumber", trackingNumber),
			zap.String("carrier", req.Carrier),
		)
		c.writeError(w, r, http.StatusInternalServerError, "tracking failed", err.Error())
		return
	}

	currentStatus := string(tracking.NormalizedStatus)

	if req.Notifications != nil && c.NotificationService != nil && currentStatus != req.PreviousStatus {
		event, ok := statusToEvent(tracking.NormalizedStatus)
		if ok {
			prefs := notificationPrefsFrom(req.Notifications)
			payload := notification.Payload{
				ShipmentID:        tracking.ShipmentID,
				TrackingNumber:    tracking.TrackingNumber,
				Carrier:           tracking.Carrier,
				Status:            currentStatus,
				PreviousStatus:    req.PreviousStatus,
				Timestamp:         time.Now().UTC(),
				EstimatedDelivery: tracking.EstimatedDelivery,
			}
			sent, failed := c.NotificationService.Dispatch(r.Context(), event, prefs, payload)
			log.Info("notification dispatch complete",
				zap.String("trackingNumber", trackingNumber),
				zap.Stringer("event", event),
				zap.Int("sent", len(sent)),
				zap.Int("failed", len(failed)),
			)
			tracking.NotificationsSent = notificationRecordsFrom(sent)
			tracking.NotificationsFailed = notificationRecordsFrom(failed)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tracking); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}

// validateTrackAndNotifyRequest checks that all required fields are present.
func validateTrackAndNotifyRequest(req trackAndNotifyRequest) error {
	if req.Carrier == "" {
		return fmt.Errorf("carrier is required")
	}
	if req.Notifications != nil && req.Notifications.WebhookURL == "" {
		return fmt.Errorf("notifications.webhookUrl is required when notifications are provided")
	}
	return nil
}

// statusToEvent maps a normalised TrackingStatus to the corresponding
// notification.Event. Returns false for statuses that do not trigger a notification
// (e.g. StatusUnknown, StatusBooked which is handled at booking time).
func statusToEvent(s adapter.TrackingStatus) (notification.Event, bool) {
	m := map[adapter.TrackingStatus]notification.Event{
		adapter.StatusPickedUp:       notification.EventPickedUp,
		adapter.StatusInTransit:      notification.EventInTransit,
		adapter.StatusOutForDelivery: notification.EventOutForDelivery,
		adapter.StatusDelivered:      notification.EventDelivered,
		adapter.StatusFailed:         notification.EventFailed,
		adapter.StatusReturned:       notification.EventReturned,
		adapter.StatusDelayed:        notification.EventDelayed,
	}
	e, ok := m[s]
	return e, ok
}
