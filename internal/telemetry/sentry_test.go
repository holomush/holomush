// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadSentryEnv(t *testing.T) {
	tests := []struct {
		name             string
		env              map[string]string
		wantOK           bool
		wantDSN          string
		wantEnvironment  string
		wantRelease      string
		wantSampleRate   float64
	}{
		{
			name:   "disabled when DSN unset",
			env:    map[string]string{"SENTRY_DSN": ""},
			wantOK: false,
		},
		{
			name: "full config parsed",
			env: map[string]string{
				"SENTRY_DSN":                "https://key@o0.ingest.sentry.io/0",
				"SENTRY_ENVIRONMENT":        "staging",
				"SENTRY_RELEASE":            "v1.2.3",
				"SENTRY_TRACES_SAMPLE_RATE": "0.25",
			},
			wantOK:          true,
			wantDSN:         "https://key@o0.ingest.sentry.io/0",
			wantEnvironment: "staging",
			wantRelease:     "v1.2.3",
			wantSampleRate:  0.25,
		},
		{
			name: "sample rate defaults to 1.0 when unset",
			env: map[string]string{
				"SENTRY_DSN": "https://key@o0.ingest.sentry.io/0",
			},
			wantOK:         true,
			wantDSN:        "https://key@o0.ingest.sentry.io/0",
			wantSampleRate: 1.0,
		},
		{
			name: "invalid sample rate falls back to 1.0",
			env: map[string]string{
				"SENTRY_DSN":                "https://key@o0.ingest.sentry.io/0",
				"SENTRY_TRACES_SAMPLE_RATE": "not-a-number",
			},
			wantOK:         true,
			wantDSN:        "https://key@o0.ingest.sentry.io/0",
			wantSampleRate: 1.0,
		},
		{
			name: "out-of-range sample rate falls back to 1.0",
			env: map[string]string{
				"SENTRY_DSN":                "https://key@o0.ingest.sentry.io/0",
				"SENTRY_TRACES_SAMPLE_RATE": "2.5",
			},
			wantOK:         true,
			wantDSN:        "https://key@o0.ingest.sentry.io/0",
			// Out-of-range values fall back to the 1.0 default; explicit
			// clamping would mask operator typos that should be visible
			// at startup.
			wantSampleRate: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all SENTRY_* keys first so omitted-from-tt.env keys
			// can't leak from the runner environment and turn an
			// assertion flaky on a CI box that happens to have one set.
			for _, key := range []string{
				"SENTRY_DSN",
				"SENTRY_ENVIRONMENT",
				"SENTRY_RELEASE",
				"SENTRY_TRACES_SAMPLE_RATE",
			} {
				t.Setenv(key, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg, ok := readSentryEnv()
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}

			assert.Equal(t, tt.wantDSN, cfg.DSN)
			assert.Equal(t, tt.wantEnvironment, cfg.Environment)
			assert.Equal(t, tt.wantRelease, cfg.Release)
			assert.InDelta(t, tt.wantSampleRate, cfg.TracesSampleRate, 1e-9)
		})
	}
}
