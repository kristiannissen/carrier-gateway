// Package notification provides event-driven webhook dispatch for shipment events.
// This file is located at /internal/notification/webhook.go.
package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Sender dispatches a notification payload to a single endpoint.
// Defined at the consumer (Service) side; implementations are injected.
type Sender interface {
	// Send POSTs payload to url, signing it with hmacSecret when non-empty.
	Send(ctx context.Context, url, hmacSecret string, payload Payload) error
}

// HTTPSender is the production Sender implementation.
// It POSTs JSON to the webhook URL and attaches an HMAC-SHA256 signature
// in the X-Signature header when a secret is provided.
type HTTPSender struct {
	client *http.Client
}

// NewHTTPSender returns an HTTPSender with a conservative timeout.
func NewHTTPSender() *HTTPSender {
	return &HTTPSender{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send implements Sender.
func (s *HTTPSender) Send(ctx context.Context, url, hmacSecret string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if hmacSecret != "" {
		req.Header.Set("X-Signature", "sha256="+sign(body, hmacSecret))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
}

// sign returns the hex-encoded HMAC-SHA256 of body using secret.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body) //nolint:errcheck
	return hex.EncodeToString(mac.Sum(nil))
}
