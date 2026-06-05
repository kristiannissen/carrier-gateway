// Package middleware provides HTTP middleware for the logistics-gateway API.
// This file is located at /internal/middleware/logging.go.
package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// sensitiveJSONFields is the set of JSON field names whose values are
// redacted before a payload is logged. Matching is case-insensitive at
// scrub time.
var sensitiveJSONFields = map[string]bool{
	"password":  true,
	"token":     true,
	"apikey":    true,
	"apiKey":    true,
	"secret":    true,
	"authorization": true,
}

// responseCapture wraps http.ResponseWriter to record the status code and
// response body without interfering with the normal write path.
type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.status = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

// LogPayloads returns middleware that logs request and response payloads at
// the Debug level. Because zap gates Debug calls before any work is done,
// scrubbing only runs when the logger is actually configured at Debug level —
// there is no cost in production unless debug logging is explicitly enabled.
//
// Sensitive JSON fields (password, token, apiKey, secret, authorization) are
// replaced with "[redacted]" and the Authorization header is replaced with
// its SHA-256 hash before the log entry is written.
func LogPayloads(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Read and restore request body.
			var reqBody []byte
			if r.Body != nil {
				reqBody, _ = io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(reqBody))
			}

			rc := &responseCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rc, r)

			// Level check before any scrubbing work.
			if !log.Core().Enabled(zapcore.DebugLevel) {
				return
			}

			requestID := FromContext(r.Context())

			log.Debug("request/response payload",
				zap.String("requestID", requestID),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rc.status),
				zap.Duration("duration", time.Since(start)),
				zap.String("authorization", hashHeader(r.Header.Get("Authorization"))),
				zap.String("requestBody", scrubJSON(reqBody)),
				zap.String("responseBody", scrubJSON(rc.body.Bytes())),
			)
		})
	}
}

// hashHeader returns the SHA-256 hex digest of s, or an empty string if s is
// empty. This lets engineers confirm which credential was used without
// exposing the value itself.
func hashHeader(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s)))
}

// scrubJSON parses b as JSON and replaces values of sensitive keys with
// "[redacted]". Non-JSON bodies are returned as-is. An empty slice returns
// an empty string.
func scrubJSON(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	var raw interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		// Not JSON — safe to log as plain text.
		return string(b)
	}

	scrubValue(raw)

	out, err := json.Marshal(raw)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// scrubValue walks the parsed JSON tree and redacts sensitive fields in place.
func scrubValue(v interface{}) {
	switch node := v.(type) {
	case map[string]interface{}:
		for k, val := range node {
			if sensitiveJSONFields[k] {
				node[k] = "[redacted]"
				continue
			}
			scrubValue(val)
		}
	case []interface{}:
		for _, item := range node {
			scrubValue(item)
		}
	}
}
