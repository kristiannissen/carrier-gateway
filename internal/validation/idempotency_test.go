// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/idempotency_test.go.
package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateIdempotencyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		key         string
		wantErr     bool
		errContains string
	}{
		{name: "empty key is valid", key: ""},
		{name: "short key is valid", key: "order-123"},
		{name: "exactly 64 chars is valid", key: strings.Repeat("a", 64)},
		{
			name:        "65 chars exceeds limit",
			key:         strings.Repeat("a", 65),
			wantErr:     true,
			errContains: "idempotency key must be 64 characters or fewer",
		},
		{
			name:        "100 chars exceeds limit",
			key:         strings.Repeat("x", 100),
			wantErr:     true,
			errContains: "got 100",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateIdempotencyKey(tc.key)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
