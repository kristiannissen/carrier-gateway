// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/idempotency_test.go.
package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// captureBody is a test handler that reads and stores the request body and
// idempotency key from context for assertion.
func captureBody(t *testing.T, body *map[string]interface{}, ctxKey *string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if len(raw) > 0 {
			require.NoError(t, json.Unmarshal(raw, body))
		}
		*ctxKey = IdempotencyKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func newIdempotencyMiddleware() func(http.Handler) http.Handler {
	return Idempotency(zap.NewNop())
}

func TestIdempotency_NoHeaderNoBody_PassesThrough(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	var ctxKey string

	h := newIdempotencyMiddleware()(captureBody(t, &body, &ctxKey))
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord"}`))
	h.ServeHTTP(httptest.NewRecorder(), r)

	assert.Empty(t, ctxKey)
	assert.NotContains(t, body, "idempotencyKey")
}

func TestIdempotency_HeaderOnly_InjectedIntoBody(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	var ctxKey string

	h := newIdempotencyMiddleware()(captureBody(t, &body, &ctxKey))
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord"}`))
	r.Header.Set(IdempotencyHeader, "order-12345")

	h.ServeHTTP(httptest.NewRecorder(), r)

	assert.Equal(t, "order-12345", ctxKey)
	assert.Equal(t, "order-12345", body["idempotencyKey"])
}

func TestIdempotency_BodyOnly_PassesThrough(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	var ctxKey string

	h := newIdempotencyMiddleware()(captureBody(t, &body, &ctxKey))
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord","idempotencyKey":"body-key-456"}`))

	h.ServeHTTP(httptest.NewRecorder(), r)

	// No header — context key is empty; body key passes through unchanged.
	assert.Empty(t, ctxKey)
	assert.Equal(t, "body-key-456", body["idempotencyKey"])
}

func TestIdempotency_HeaderAndBodyMatch_Accepted(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	var ctxKey string

	h := newIdempotencyMiddleware()(captureBody(t, &body, &ctxKey))
	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord","idempotencyKey":"matching-key"}`))
	r.Header.Set(IdempotencyHeader, "matching-key")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "matching-key", ctxKey)
	assert.Equal(t, "matching-key", body["idempotencyKey"])
}

func TestIdempotency_HeaderAndBodyMismatch_Returns422(t *testing.T) {
	t.Parallel()

	h := newIdempotencyMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called on mismatch")
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord","idempotencyKey":"body-key"}`))
	r.Header.Set(IdempotencyHeader, "header-key")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "conflict")
}

func TestIdempotency_HeaderTooLong_Returns400(t *testing.T) {
	t.Parallel()

	h := newIdempotencyMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when header is too long")
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord"}`))
	r.Header.Set(IdempotencyHeader, strings.Repeat("x", 65))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["details"], "64 characters or fewer")
}

func TestIdempotency_HeaderExactly64Chars_Accepted(t *testing.T) {
	t.Parallel()

	var ctxKey string
	h := newIdempotencyMiddleware()(captureBody(t, &map[string]interface{}{}, &ctxKey))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		strings.NewReader(`{"carrier":"postnord"}`))
	r.Header.Set(IdempotencyHeader, strings.Repeat("a", 64))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, strings.Repeat("a", 64), ctxKey)
}

func TestIdempotency_NonPostRequest_PassesThrough(t *testing.T) {
	t.Parallel()

	called := false
	h := newIdempotencyMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/trackings/PN123", nil)
	r.Header.Set(IdempotencyHeader, "some-key")

	h.ServeHTTP(httptest.NewRecorder(), r)
	assert.True(t, called, "GET handler should be called without modification")
}

func TestIdempotency_NonJSONBody_PassesThrough(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	var ctxKey string

	h := newIdempotencyMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		ctxKey = IdempotencyKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings",
		bytes.NewBufferString("not-json-payload"))
	r.Header.Set(IdempotencyHeader, "key-abc")

	h.ServeHTTP(httptest.NewRecorder(), r)

	// Key stored on context; non-JSON body passed through unchanged.
	assert.Equal(t, "key-abc", ctxKey)
	assert.Equal(t, "not-json-payload", string(capturedBody))
}

func TestIdempotencyKeyFromContext_EmptyWhenNotSet(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, IdempotencyKeyFromContext(r.Context()))
}
