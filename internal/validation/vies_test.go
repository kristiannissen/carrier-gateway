// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/vies_test.go.
package validation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All VIES tests live inside TestValidateVATNumberLive so they run
// sequentially. The package-level viesBaseURL variable is mutated by each
// subtest; parallel subtests would race on that write. Nesting under a single
// parent test is the standard Go pattern for serialising subtests that share
// mutable package state.
func TestValidateVATNumberLive(t *testing.T) {
	// Helper: redirect viesBaseURL to srv and restore on cleanup.
	use := func(t *testing.T, srv *httptest.Server) {
		t.Helper()
		original := viesBaseURL
		viesBaseURL = srv.URL
		t.Cleanup(func() {
			viesBaseURL = original
			srv.Close()
		})
	}

	responder := func(status int, isValid bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(viesResponse{IsValid: isValid})
		}))
	}

	hanging := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
	}

	// Non-EU countries must return immediately without any network call.
	// No server mutation — included here so it runs sequentially with the rest.
	t.Run("non_EU_country_passthrough", func(t *testing.T) {
		for _, country := range []string{"NO", "GB", "CH", "US", "CA", "AU", "JP", "CN"} {
			country := country
			t.Run(country, func(t *testing.T) {
				valid, unavailable, err := ValidateVATNumberLive(t.Context(), "123456789", country)
				assert.True(t, valid)
				assert.False(t, unavailable)
				assert.NoError(t, err)
			})
		}
	})

	t.Run("valid_number", func(t *testing.T) {
		use(t, responder(http.StatusOK, true))
		valid, unavailable, err := ValidateVATNumberLive(t.Context(), "DE123456789", "DE")
		assert.True(t, valid)
		assert.False(t, unavailable)
		assert.NoError(t, err)
	})

	t.Run("invalid_number", func(t *testing.T) {
		use(t, responder(http.StatusOK, false))
		valid, unavailable, err := ValidateVATNumberLive(t.Context(), "DE000000000", "DE")
		assert.False(t, valid)
		assert.False(t, unavailable)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not registered as active in VIES")
	})

	t.Run("vies_returns_503", func(t *testing.T) {
		use(t, responder(http.StatusServiceUnavailable, false))
		valid, unavailable, err := ValidateVATNumberLive(t.Context(), "DE123456789", "DE")
		assert.False(t, valid)
		assert.True(t, unavailable)
		assert.NoError(t, err)
	})

	t.Run("vies_returns_500", func(t *testing.T) {
		use(t, responder(http.StatusInternalServerError, false))
		valid, unavailable, err := ValidateVATNumberLive(t.Context(), "SE1234567890", "SE")
		assert.False(t, valid)
		assert.True(t, unavailable)
		assert.NoError(t, err)
	})

	t.Run("timeout", func(t *testing.T) {
		use(t, hanging())
		start := time.Now()
		valid, unavailable, err := ValidateVATNumberLive(t.Context(), "DK12345678", "DK")
		elapsed := time.Since(start)
		assert.Less(t, elapsed, viesTimeout+500*time.Millisecond)
		assert.False(t, valid)
		assert.True(t, unavailable)
		assert.NoError(t, err)
	})

	t.Run("parent_context_cancelled", func(t *testing.T) {
		use(t, hanging())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		valid, unavailable, err := ValidateVATNumberLive(ctx, "DK12345678", "DK")
		assert.False(t, valid)
		assert.True(t, unavailable)
		assert.NoError(t, err)
	})

	t.Run("country_prefix_stripped", func(t *testing.T) {
		var receivedPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(viesResponse{IsValid: true})
		}))
		use(t, srv)
		_, _, _ = ValidateVATNumberLive(t.Context(), "SE1234567890", "SE")
		// VIES URL must be /SE/vat/1234567890 — country prefix stripped.
		assert.Equal(t, "/SE/vat/1234567890", receivedPath)
	})

	t.Run("number_without_prefix", func(t *testing.T) {
		var receivedPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(viesResponse{IsValid: true})
		}))
		use(t, srv)
		// DK VAT numbers have no country prefix — passed as-is.
		_, _, _ = ValidateVATNumberLive(t.Context(), "12345678", "DK")
		assert.Equal(t, "/DK/vat/12345678", receivedPath)
	})
}
