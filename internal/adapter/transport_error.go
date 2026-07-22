// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/transport_error.go.
package adapter

import (
	"errors"
	"net/url"
)

// sanitizeTransportError strips query-string credentials from the request URL
// embedded in a *url.Error before the error is wrapped and potentially
// surfaced to a caller of the gateway.
//
// PostNord and DAO authenticate by passing their API key as a URL query
// parameter rather than a header. When the underlying HTTP transport fails
// (timeout, DNS failure, connection refused, TLS handshake failure), Go's
// *url.Error embeds the full request URL — including that query string — in
// its Error() string. Without this scrub, a routine network hiccup talking to
// the carrier would leak a live API key through the wrapped error chain to
// whoever called the gateway.
//
// Adapters that authenticate via headers (Bearer, Basic) are unaffected:
// url.Error never includes headers, only the URL.
func sanitizeTransportError(err error) error {
	var uErr *url.Error
	if errors.As(err, &uErr) && uErr.URL != "" {
		if u, parseErr := url.Parse(uErr.URL); parseErr == nil && u.RawQuery != "" {
			u.RawQuery = "REDACTED"
			uErr.URL = u.String()
		}
	}
	return err
}
