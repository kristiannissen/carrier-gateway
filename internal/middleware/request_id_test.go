// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/request_id_test.go.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID_GeneratesIDWhenAbsent(t *testing.T) {
	t.Parallel()

	var capturedID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = FromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	require.NotEmpty(t, capturedID)
	assert.Len(t, capturedID, 32) // 16 bytes → 32 hex chars
	assert.Equal(t, capturedID, w.Result().Header.Get(RequestIDHeader))
}

func TestRequestID_ForwardsExistingID(t *testing.T) {
	t.Parallel()

	const existingID = "abc123"
	var capturedID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = FromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(RequestIDHeader, existingID)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, existingID, capturedID)
	assert.Equal(t, existingID, w.Result().Header.Get(RequestIDHeader))
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	t.Parallel()

	ids := make([]string, 10)
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, FromContext(r.Context()))
	}))

	for range ids {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}

	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		_, duplicate := seen[id]
		assert.False(t, duplicate, "duplicate request ID: %s", id)
		seen[id] = struct{}{}
	}
}

func TestFromContext_EmptyWhenNotSet(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, FromContext(r.Context()))
}
