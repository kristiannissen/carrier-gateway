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
	"net"
	"net/http"
	"net/url"
	"strings"
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
	// validateHost checks whether webhookURL's resolved address is a
	// permitted SSRF destination. Defaults to validateWebhookHost via
	// NewHTTPSender. A nil value (as when a test in this package builds an
	// HTTPSender{} literal directly, e.g. to dial an httptest.NewTLSServer
	// on 127.0.0.1) skips the check — production code always goes through
	// NewHTTPSender, so the check is never silently absent there.
	validateHost func(ctx context.Context, webhookURL string) error
}

// NewHTTPSender returns an HTTPSender with a conservative timeout and SSRF
// host validation enabled.
func NewHTTPSender() *HTTPSender {
	return &HTTPSender{
		client:       &http.Client{Timeout: 10 * time.Second},
		validateHost: validateWebhookHost,
	}
}

// Send implements Sender.
func (s *HTTPSender) Send(ctx context.Context, webhookURL, hmacSecret string, payload Payload) error {
	if !strings.HasPrefix(webhookURL, "https://") {
		return fmt.Errorf("webhook url must use https: %s", webhookURL)
	}

	// Reject webhook destinations that resolve to loopback, link-local, or
	// private network addresses. The webhook URL is supplied by whoever
	// calls the gateway; without this check the gateway can be used as an
	// SSRF proxy into its own network (e.g. cloud metadata endpoints, admin
	// interfaces on localhost or the container's private subnet).
	if s.validateHost != nil {
		if err := s.validateHost(ctx, webhookURL); err != nil {
			return fmt.Errorf("webhook url rejected: %w", err)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Type", string(payload.Event))

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

// isPrivateOrReservedIP reports whether ip must not be used as an outbound
// webhook destination: loopback, link-local (this range also covers the
// 169.254.169.254 cloud metadata address), unspecified, or RFC 1918 / ULA
// private ranges.
func isPrivateOrReservedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsPrivate()
}

// validateWebhookHost resolves the hostname in rawURL and rejects it if any
// resolved address is loopback, link-local, or private — the standard
// blast-radius reduction against SSRF for a server that dials a
// caller-supplied URL.
//
// This is a point-in-time check: the connection is not pinned to the
// resolved address, so a DNS answer that changes between this check and the
// actual dial (DNS rebinding) could bypass it. That residual risk is accepted
// here — closing it fully would require a custom net.Dialer that re-validates
// the address it is about to connect to, which is more machinery than this
// stateless gateway's threat model currently justifies.
func validateWebhookHost(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook url: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook url has no host")
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("webhook url resolves to a private or reserved address")
		}
		return nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve webhook host: %w", err)
	}
	for _, addr := range addrs {
		if isPrivateOrReservedIP(addr.IP) {
			return fmt.Errorf("webhook url resolves to a private or reserved address")
		}
	}
	return nil
}
