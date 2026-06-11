// Package notification provides event-driven webhook dispatch for shipment events.
// This file is located at /internal/notification/notification.go.
package notification

import "time"

// Event identifies a shipment lifecycle event that may trigger notifications.
type Event string

const (
	// EventBooked means the shipment has been booked with the carrier.
	EventBooked Event = "booked"
	// EventPickedUp means the carrier has collected the parcel.
	EventPickedUp Event = "picked_up"
	// EventInTransit means the parcel is moving through the carrier network.
	EventInTransit Event = "in_transit"
	// EventOutForDelivery means the parcel is on the delivery vehicle.
	EventOutForDelivery Event = "out_for_delivery"
	// EventDelivered means the parcel has been delivered.
	EventDelivered Event = "delivered"
	// EventDelayed means the parcel is delayed relative to the original ETA.
	EventDelayed Event = "delayed"
	// EventFailed means a delivery attempt failed.
	EventFailed Event = "failed"
	// EventReturned means the parcel is being returned to the sender.
	EventReturned Event = "returned"
)

// WebhookPrefs holds the integrator-provided webhook configuration.
// The gateway POSTs the event payload to URL and signs it with Secret.
// When Events is non-empty, notifications are only dispatched for the
// listed events; an empty slice means all events are dispatched.
type WebhookPrefs struct {
	// URL is the endpoint that receives the event payload.
	URL string `json:"url"`
	// Secret is used to compute the HMAC-SHA256 signature sent in
	// the X-Signature header. Leave empty to skip signing.
	Secret string `json:"secret,omitempty"`
	// Events filters which events trigger a dispatch.
	// An empty slice means all events are dispatched.
	Events []Event `json:"events,omitempty"`
}

// Preferences holds the notification configuration supplied by the integrator.
type Preferences struct {
	Webhook *WebhookPrefs `json:"webhook,omitempty"`
}

// Record describes the outcome of a single notification attempt.
type Record struct {
	// Event is the shipment lifecycle event that triggered this notification.
	Event Event `json:"event"`
	// Channel is always "webhook" for now.
	Channel string `json:"channel"`
	// URL is the webhook endpoint that was called.
	URL string `json:"url"`
	// Status is "sent" or "failed".
	Status string `json:"status"`
	// Error is set when Status is "failed".
	Error string `json:"error,omitempty"`
	// Timestamp is when the dispatch was attempted.
	Timestamp time.Time `json:"timestamp"`
}

// Payload is the JSON body POSTed to the integrator's webhook URL.
type Payload struct {
	// Event is the shipment lifecycle event.
	Event Event `json:"event"`
	// ShipmentID is the carrier-level shipment identifier, if available.
	// For PostNord this is the shipmentId; for Bring it is the consignment number.
	ShipmentID string `json:"shipmentId,omitempty"`
	// TrackingNumber identifies the shipment.
	TrackingNumber string `json:"trackingNumber"`
	// Carrier is the carrier key (e.g. "postnord", "bring").
	Carrier string `json:"carrier"`
	// Status is the current normalised shipment status (e.g. "delivered").
	Status string `json:"status"`
	// PreviousStatus is the normalised status before this event, if known.
	PreviousStatus string `json:"previousStatus,omitempty"`
	// Timestamp is when the status change was detected.
	Timestamp time.Time `json:"timestamp"`
	// EstimatedDelivery is the carrier-provided ETA, if available.
	EstimatedDelivery string `json:"estimatedDelivery,omitempty"`
	// DelayReason is set when Event is EventDelayed.
	DelayReason string `json:"delayReason,omitempty"`
}
