// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestSeedULIDIsValid(t *testing.T) {
	// The well-known seed ULID used for idempotency
	// Must be exactly 26 characters using Crockford's base32 alphabet
	seedULID := "01HZN3XS000000000000000000"

	require.Len(t, seedULID, 26, "seed ULID must be exactly 26 characters")

	id, err := ulid.Parse(seedULID)
	require.NoError(t, err, "seed ULID should be valid")
	require.NotEqual(t, ulid.ULID{}, id, "parsed ULID should not be zero")
}

func TestRunSeed_MissingDatabaseURL(t *testing.T) {
	// Clear DATABASE_URL to test missing config
	t.Setenv("DATABASE_URL", "")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})

	cfg := &seedConfig{timeout: 30 * time.Second}
	err := runSeed(cmd, nil, cfg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONFIG_INVALID")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestRunSeed_InvalidDatabaseURL(t *testing.T) {
	// Use a malformed connection string that will fail during parsing/connection
	// Using an invalid scheme forces an early failure
	t.Setenv("DATABASE_URL", "invalid://not-a-valid-url")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})

	cfg := &seedConfig{timeout: 30 * time.Second}
	err := runSeed(cmd, nil, cfg)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DB_CONNECT_FAILED")
}

func TestNewSeedCmd(t *testing.T) {
	cmd := NewSeedCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "seed", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
}

func TestNewSeedCmd_TimeoutFlag(t *testing.T) {
	cmd := NewSeedCmd()

	// Verify timeout flag exists with correct default
	timeout, err := cmd.Flags().GetDuration("timeout")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, timeout, "default timeout should be 30s")

	// Verify custom timeout can be set
	require.NoError(t, cmd.Flags().Set("timeout", "1m"))
	timeout, err = cmd.Flags().GetDuration("timeout")
	require.NoError(t, err)
	assert.Equal(t, time.Minute, timeout, "timeout should be settable to 1m")
}

func TestNewSeedCmd_NoStrictFlag(t *testing.T) {
	cmd := NewSeedCmd()

	// Verify --no-strict flag exists with correct default (false = strict mode by default)
	noStrict, err := cmd.Flags().GetBool("no-strict")
	require.NoError(t, err)
	assert.False(t, noStrict, "default should be strict mode (no-strict=false)")

	// Verify flag can be set
	require.NoError(t, cmd.Flags().Set("no-strict", "true"))
	noStrict, err = cmd.Flags().GetBool("no-strict")
	require.NoError(t, err)
	assert.True(t, noStrict, "no-strict should be settable to true")
}

func TestSeedConfig_NoStrictField(t *testing.T) {
	// Verify seedConfig has noStrict field
	cfg := &seedConfig{
		noStrict: true,
	}
	assert.True(t, cfg.noStrict)
}

func TestCheckSeedMismatches(t *testing.T) {
	tests := []struct {
		name          string
		noStrict      bool
		hasMismatches bool
		wantError     bool
		wantWarnings  bool
	}{
		{
			name:          "strict mode with mismatches returns error",
			noStrict:      false,
			hasMismatches: true,
			wantError:     true,
			wantWarnings:  true,
		},
		{
			name:          "strict mode without mismatches returns success",
			noStrict:      false,
			hasMismatches: false,
			wantError:     false,
			wantWarnings:  false,
		},
		{
			name:          "no-strict mode with mismatches returns success but warns",
			noStrict:      true,
			hasMismatches: true,
			wantError:     false,
			wantWarnings:  true,
		},
		{
			name:          "no-strict mode without mismatches returns success",
			noStrict:      true,
			hasMismatches: false,
			wantError:     false,
			wantWarnings:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errBuf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetErr(&errBuf)

			var mismatches []string
			if tt.hasMismatches {
				mismatches = []string{
					"name mismatch: expected 'The Nexus', got 'Old Name'",
				}
			}

			err := checkSeedMismatches(cmd, mismatches, tt.noStrict)

			if tt.wantError {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "SEED_MISMATCH")
			} else {
				require.NoError(t, err)
			}

			if tt.wantWarnings {
				assert.Contains(t, errBuf.String(), "WARNING:")
				assert.Contains(t, errBuf.String(), "name mismatch")
			} else {
				assert.Empty(t, errBuf.String())
			}
		})
	}
}

func TestLogVerificationFailure(t *testing.T) {
	var logBuf bytes.Buffer
	var errBuf bytes.Buffer

	// Set up a logger that writes to our buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	original := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(original)

	id, _ := ulid.Parse("01HZN3XS000000000000000000")
	testErr := errors.New("database connection lost")

	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	// This function encapsulates the verification failure logging behavior
	logVerificationFailure(cmd, id, testErr)

	// Verify ERROR level was used (not WARN)
	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(logBuf.Bytes(), &logEntry), "Failed to parse log JSON: %s", logBuf.String())
	assert.Equal(t, "ERROR", logEntry["level"], "verification failure should log at ERROR level")
	assert.Equal(t, "Could not verify existing seed location", logEntry["msg"])
	assert.Contains(t, logEntry["location_id"], "01HZN3XS000000000000000000")
	assert.Contains(t, logEntry["error"], "database connection lost")

	// Verify stderr warning was printed for operator visibility
	assert.Contains(t, errBuf.String(), "WARNING:", "should print warning to stderr")
	assert.Contains(t, errBuf.String(), "Could not verify existing seed location", "stderr should describe the issue")
}

func TestCollectMismatches(t *testing.T) {
	id, _ := ulid.Parse("01HZN3XS000000000000000000")

	tests := []struct {
		name         string
		expected     seedLocation
		actual       seedLocation
		wantCount    int
		wantContains []string
	}{
		{
			name: "no mismatches",
			expected: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "A description",
			},
			actual: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "A description",
			},
			wantCount: 0,
		},
		{
			name: "name mismatch",
			expected: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "A description",
			},
			actual: seedLocation{
				Name:        "Different Name",
				Type:        "persistent",
				Description: "A description",
			},
			wantCount:    1,
			wantContains: []string{"name", "The Nexus", "Different Name"},
		},
		{
			name: "type mismatch",
			expected: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "A description",
			},
			actual: seedLocation{
				Name:        "The Nexus",
				Type:        "temporary",
				Description: "A description",
			},
			wantCount:    1,
			wantContains: []string{"type", "persistent", "temporary"},
		},
		{
			name: "description mismatch",
			expected: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "Expected description",
			},
			actual: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "Different description",
			},
			wantCount:    1,
			wantContains: []string{"description"},
		},
		{
			name: "multiple mismatches",
			expected: seedLocation{
				Name:        "The Nexus",
				Type:        "persistent",
				Description: "Expected",
			},
			actual: seedLocation{
				Name:        "Other",
				Type:        "temporary",
				Description: "Different",
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatches := collectMismatches(id, tt.expected, tt.actual)
			assert.Len(t, mismatches, tt.wantCount)
			for _, want := range tt.wantContains {
				found := false
				for _, m := range mismatches {
					if bytes.Contains([]byte(m), []byte(want)) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected mismatch to contain %q", want)
				}
			}
		})
	}
}
