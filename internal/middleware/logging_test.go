// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/logging_test.go.
package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newObservedLogger returns a zap logger that captures log entries and an
// observer that can be queried in tests.
func newObservedLogger(level zapcore.Level) (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(level)
	return zap.New(core), logs
}

func TestLogPayloads_LoggedAtDebugLevel(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings", bytes.NewBufferString(`{"carrier":"postnord"}`))
	r.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len(), "expected one debug log entry")
	entry := logs.All()[0]
	assert.Equal(t, "request/response payload", entry.Message)
	assert.Equal(t, zapcore.DebugLevel, entry.Level)

	fields := fieldMap(entry.Context)
	assert.Equal(t, "POST", fields["method"])
	assert.Equal(t, "/api/bookings", fields["path"])
	assert.Equal(t, int64(200), fields["status"])
	assert.Contains(t, fields["requestBody"], "postnord")
	assert.Contains(t, fields["responseBody"], "ok")
}

func TestLogPayloads_NotLoggedAboveDebugLevel(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.InfoLevel)

	handler := LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	assert.Equal(t, 0, logs.Len(), "no log entries expected when level is Info")
}

func TestLogPayloads_SensitiveJSONFieldsRedacted(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	body := `{"carrier":"postnord","password":"s3cr3t","token":"abc123","apiKey":"key-xyz","secret":"topsecret"}`

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"resp-token","status":"ok"}`))
	})))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)

	reqBody := fields["requestBody"].(string)
	assert.Contains(t, reqBody, "[redacted]")
	assert.NotContains(t, reqBody, "s3cr3t")
	assert.NotContains(t, reqBody, "abc123")
	assert.NotContains(t, reqBody, "key-xyz")
	assert.NotContains(t, reqBody, "topsecret")
	assert.Contains(t, reqBody, "postnord") // non-sensitive field preserved

	respBody := fields["responseBody"].(string)
	assert.Contains(t, respBody, "[redacted]")
	assert.NotContains(t, respBody, "resp-token")
}

func TestLogPayloads_AuthorizationHeaderHashed(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/api/trackings/PN123", nil)
	r.Header.Set("Authorization", "Bearer super-secret-token")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)

	authField := fields["authorization"].(string)
	assert.True(t, strings.HasPrefix(authField, "sha256:"), "authorization should be sha256 hashed")
	assert.NotContains(t, authField, "super-secret-token")
}

func TestLogPayloads_EmptyAuthorizationHeader(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)
	assert.Equal(t, "", fields["authorization"])
}

func TestLogPayloads_NonJSONBodyPassedThrough(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader("not-json-payload"))
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)
	assert.Equal(t, "not-json-payload", fields["requestBody"])
}

func TestLogPayloads_RequestIDPresentInLogEntry(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set(RequestIDHeader, "test-request-id-123")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)
	assert.Equal(t, "test-request-id-123", fields["requestID"])
}

func TestLogPayloads_NestedSensitiveFieldsRedacted(t *testing.T) {
	t.Parallel()

	log, logs := newObservedLogger(zapcore.DebugLevel)

	body := `{"shipment":{"sender":{"apiKey":"nested-key","name":"Test"},"password":"nested-pass"}}`

	handler := RequestID(LogPayloads(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodPost, "/api/bookings", strings.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.Equal(t, 1, logs.Len())
	fields := fieldMap(logs.All()[0].Context)
	reqBody := fields["requestBody"].(string)
	assert.NotContains(t, reqBody, "nested-key")
	assert.NotContains(t, reqBody, "nested-pass")
	assert.Contains(t, reqBody, "Test") // non-sensitive field preserved
}

func TestHashHeader(t *testing.T) {
	t.Parallel()

	assert.Empty(t, hashHeader(""))

	h := hashHeader("Bearer my-token")
	assert.True(t, strings.HasPrefix(h, "sha256:"))
	assert.Len(t, h, len("sha256:")+64) // sha256 = 32 bytes = 64 hex chars

	// Same input always produces the same hash.
	assert.Equal(t, hashHeader("Bearer my-token"), hashHeader("Bearer my-token"))

	// Different inputs produce different hashes.
	assert.NotEqual(t, hashHeader("Bearer my-token"), hashHeader("Bearer other-token"))
}

func TestScrubJSON_EmptyInput(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", scrubJSON(nil))
	assert.Equal(t, "", scrubJSON([]byte{}))
}

// fieldMap converts a zap field slice into a map for easy assertion access.
func fieldMap(fields []zapcore.Field) map[string]interface{} {
	m := make(map[string]interface{}, len(fields))
	for _, f := range fields {
		switch f.Type {
		case zapcore.StringType:
			m[f.Key] = f.String
		case zapcore.Int64Type, zapcore.Int32Type:
			m[f.Key] = f.Integer
		case zapcore.DurationType:
			m[f.Key] = f.Integer
		default:
			m[f.Key] = f.Interface
		}
	}
	return m
}
