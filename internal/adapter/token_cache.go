// Package adapter provides shared token-caching utilities for carrier adapters.
// This file is located at /internal/adapter/token_cache.go.
package adapter

import (
	"sync"
	"time"
)

// tokenCache holds a cached OAuth2 Bearer token and its expiry time.
// It is embedded by adapters that use OAuth2 client_credentials or ROPC flows.
type tokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not about to expire.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *tokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}
