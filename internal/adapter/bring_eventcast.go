// Package adapter provides the Bring Event Cast webhook implementation.
// This file is located at /internal/adapter/bring_eventcast.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BringEventSet is a Bring Event Cast event group identifier.
// The full list is documented at https://developer.bring.com/api/event-cast/.
//
// Coverage gaps vs notification.Event:
//
//   - EventPickedUp: the tracking statuses COLLECTED and HANDED_IN both
//     normalize to StatusPickedUp, but neither is an Event Cast group. Pickup
//     detection requires polling TrackShipment — it cannot be push-driven.
//
//   - EventDelayed: Bring has no Event Cast group for delays. DEVIATION
//     (the nearest raw status) normalizes to StatusFailed, not StatusDelayed.
//     Delay detection requires polling TrackShipment.
type BringEventSet string

const (
	// BringEventDelivered fires when the shipment is delivered to the recipient.
	BringEventDelivered BringEventSet = "DELIVERED"
	// BringEventDeliveredSender fires when a return is delivered back to the sender.
	BringEventDeliveredSender BringEventSet = "DELIVERED_SENDER"
	// BringEventDeliveryCancelled fires when a scheduled delivery is cancelled.
	BringEventDeliveryCancelled BringEventSet = "DELIVERY_CANCELLED"
	// BringEventDeliveryChanged fires when the delivery time or address changes.
	BringEventDeliveryChanged BringEventSet = "DELIVERY_CHANGED"
	// BringEventInTransit fires when the shipment moves through the Bring network.
	BringEventInTransit BringEventSet = "IN_TRANSIT"
	// BringEventPreNotified fires when the carrier notifies the recipient of an
	// upcoming delivery. Normalizes to StatusInTransit — the shipment is already
	// moving; this is not a booking event.
	BringEventPreNotified BringEventSet = "PRE_NOTIFIED"
	// BringEventReadyForPickup fires when the shipment is available at a service point.
	BringEventReadyForPickup BringEventSet = "READY_FOR_PICKUP"
	// BringEventReturn fires when the shipment enters the return flow.
	BringEventReturn BringEventSet = "RETURN"
	// BringEventTerminal fires when the shipment arrives at a Bring terminal.
	BringEventTerminal BringEventSet = "TERMINAL"
)

// BringWebhookHeader is a key/value pair sent with every Event Cast POST.
// Value is write-only: Bring never returns it in GET responses.
type BringWebhookHeader struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// BringWebhookConfig describes the HTTP endpoint that Bring will call.
type BringWebhookConfig struct {
	// WebhookURL is the HTTPS endpoint that receives event payloads.
	WebhookURL string `json:"webhookUrl"`
	// ContentType is the Content-Type header Bring sets on outbound requests.
	// Defaults to "application/json" when empty.
	ContentType string `json:"contentType"`
	// Headers are extra HTTP headers Bring includes in every dispatch.
	// Useful for bearer tokens or shared secrets.
	Headers []BringWebhookHeader `json:"headers,omitempty"`
}

// BringCustomerWebhookRequest is the body for POST /event-cast/api/v1/customer/webhooks.
// Register once per customer number; Bring fires events for every shipment under that number.
// The subscription lives for 365 days and must be renewed via RenewCustomerWebhook.
type BringCustomerWebhookRequest struct {
	// CustomerNumber is the MyBring customer account number (not the login email).
	CustomerNumber string `json:"customerNumber"`
	// EventSet lists the event groups that trigger a dispatch.
	// An empty slice is not accepted — provide at least one event.
	EventSet []BringEventSet `json:"eventSet"`
	// WebhookConfiguration describes the endpoint that receives payloads.
	WebhookConfiguration BringWebhookConfig `json:"webhookConfiguration"`
}

// BringCustomerWebhookResponse is returned by Register and Renew operations.
type BringCustomerWebhookResponse struct {
	// ID is the subscription UUID used for Delete and Renew calls.
	ID                   string             `json:"id"`
	CustomerNumber       string             `json:"customerNumber"`
	EventSet             []BringEventSet    `json:"eventSet"`
	WebhookConfiguration BringWebhookConfig `json:"webhookConfiguration"`
	// Created is when the subscription was first registered.
	Created time.Time `json:"created"`
	// Expiry is when the subscription will stop receiving events (365 days after creation or last renewal).
	Expiry time.Time `json:"expiry"`
	// CreatedBy is the email of the MyBring user who registered the subscription.
	CreatedBy string `json:"createdBy,omitempty"`
}

// RegisterCustomerWebhook creates a customer-level Event Cast subscription.
// The subscription is active for 365 days and covers every shipment sent
// by the given customer number. Call RenewCustomerWebhook before Expiry to
// extend by another 365 days.
//
// Bring returns 409 Conflict when an identical subscription already exists
// (same customer number, event set, and webhook URL). Callers should treat
// this as success and store the existing subscription ID from the error body,
// or call GetCustomerWebhooks to locate the existing entry.
func (a *BringAdapter) RegisterCustomerWebhook(ctx context.Context, req BringCustomerWebhookRequest) (*BringCustomerWebhookResponse, error) {
	if req.CustomerNumber == "" {
		return nil, fmt.Errorf("bring: customerNumber is required")
	}
	if len(req.EventSet) == 0 {
		return nil, fmt.Errorf("bring: eventSet must contain at least one event")
	}
	if req.WebhookConfiguration.WebhookURL == "" {
		return nil, fmt.Errorf("bring: webhookConfiguration.webhookUrl is required")
	}
	if req.WebhookConfiguration.ContentType == "" {
		req.WebhookConfiguration.ContentType = "application/json"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to marshal customer webhook request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/event-cast/api/v1/customer/webhooks",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to create customer webhook request: %w", err)
	}
	a.setBringHeaders(httpReq)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bring: customer webhook API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to read customer webhook response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bring: customer webhook registration returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var sub BringCustomerWebhookResponse
	if err := json.Unmarshal(respBody, &sub); err != nil {
		return nil, fmt.Errorf("bring: failed to decode customer webhook response: %w", err)
	}
	return &sub, nil
}

// DeleteCustomerWebhook removes a customer-level Event Cast subscription by ID.
// After deletion Bring stops sending events for the associated customer number.
func (a *BringAdapter) DeleteCustomerWebhook(ctx context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return fmt.Errorf("bring: subscriptionID is required")
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		fmt.Sprintf("%s/event-cast/api/v1/customer/webhooks/%s", a.BaseURL, subscriptionID),
		nil,
	)
	if err != nil {
		return fmt.Errorf("bring: failed to create customer webhook delete request: %w", err)
	}
	a.setBringHeaders(httpReq)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("bring: customer webhook delete call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck
		return fmt.Errorf("bring: customer webhook delete returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// RenewCustomerWebhook extends the expiry of an existing customer-level
// subscription by 365 days from the time of the call.
func (a *BringAdapter) RenewCustomerWebhook(ctx context.Context, subscriptionID string) (*BringCustomerWebhookResponse, error) {
	if subscriptionID == "" {
		return nil, fmt.Errorf("bring: subscriptionID is required")
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/event-cast/api/v1/customer/webhooks/renew/%s", a.BaseURL, subscriptionID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to create customer webhook renew request: %w", err)
	}
	a.setBringHeaders(httpReq)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bring: customer webhook renew call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to read customer webhook renew response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bring: customer webhook renew returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var sub BringCustomerWebhookResponse
	if err := json.Unmarshal(respBody, &sub); err != nil {
		return nil, fmt.Errorf("bring: failed to decode customer webhook renew response: %w", err)
	}
	return &sub, nil
}

// setBringHeaders sets the authentication and client-identification headers
// required on every Bring API request.
func (a *BringAdapter) setBringHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	r.Header.Set("X-MyBring-API-Key", a.APIKey)
	r.Header.Set("X-Bring-Client-URL", "https://github.com/kristiannissen/carrier-gateway")
}
