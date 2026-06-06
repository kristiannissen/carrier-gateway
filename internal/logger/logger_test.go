// Package logger provides a shared zap logger constructor.
// This file is located at /internal/logger/logger_test.go.
package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestNew_DefaultsToProductionLogger(t *testing.T) {
	t.Setenv("LOG_ENV", "")
	t.Setenv("LOG_LEVEL", "")

	log, err := New()
	require.NoError(t, err)
	require.NotNil(t, log)

	// Production logger defaults to info level — debug is disabled.
	assert.False(t, log.Core().Enabled(zapcore.DebugLevel))
	assert.True(t, log.Core().Enabled(zapcore.InfoLevel))
}

func TestNew_DevelopmentMode(t *testing.T) {
	t.Setenv("LOG_ENV", "development")

	log, err := New()
	require.NoError(t, err)
	require.NotNil(t, log)

	// Development logger enables debug level.
	assert.True(t, log.Core().Enabled(zapcore.DebugLevel))
}

func TestNew_LogLevelOverride(t *testing.T) {
	t.Setenv("LOG_ENV", "")

	cases := []struct {
		level    string
		enabled  zapcore.Level
		disabled zapcore.Level
	}{
		{"debug", zapcore.DebugLevel, zapcore.Level(99)},
		{"warn", zapcore.WarnLevel, zapcore.DebugLevel},
		{"error", zapcore.ErrorLevel, zapcore.WarnLevel},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.level, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", tc.level)

			log, err := New()
			require.NoError(t, err)
			assert.True(t, log.Core().Enabled(tc.enabled),
				"expected %s to be enabled at LOG_LEVEL=%s", tc.enabled, tc.level)
			if tc.disabled != zapcore.Level(99) {
				assert.False(t, log.Core().Enabled(tc.disabled),
					"expected %s to be disabled at LOG_LEVEL=%s", tc.disabled, tc.level)
			}
		})
	}
}

func TestNew_InvalidLogLevelFallsBackToInfo(t *testing.T) {
	t.Setenv("LOG_ENV", "")
	t.Setenv("LOG_LEVEL", "nonsense")

	log, err := New()
	require.NoError(t, err)
	require.NotNil(t, log)

	// Invalid level is silently ignored — falls back to production default (info).
	assert.True(t, log.Core().Enabled(zapcore.InfoLevel))
}
