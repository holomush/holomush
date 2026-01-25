// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestParseForceVersion(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantVersion int
		wantErr     bool
		wantErrCode string
	}{
		{
			name:        "valid integer",
			input:       "3",
			wantVersion: 3,
			wantErr:     false,
		},
		{
			name:        "zero is valid",
			input:       "0",
			wantVersion: 0,
			wantErr:     false,
		},
		{
			name:        "non-numeric returns error",
			input:       "abc",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "float parses as integer (Sscanf stops at dot)",
			input:       "1.5",
			wantVersion: 1,
			wantErr:     false,
		},
		{
			name:        "trailing chars are ignored (Sscanf stops at non-digit)",
			input:       "3abc",
			wantVersion: 3,
			wantErr:     false,
		},
		{
			name:        "negative is valid",
			input:       "-1",
			wantVersion: -1,
			wantErr:     false,
		},
		{
			name:        "empty string returns error",
			input:       "",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "whitespace only returns error",
			input:       "   ",
			wantErr:     true,
			wantErrCode: "INVALID_VERSION",
		},
		{
			name:        "leading whitespace is handled",
			input:       "  42",
			wantVersion: 42,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parseForceVersion(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				assert.Equal(t, 0, version)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVersion, version)
			}
		})
	}
}

func TestGetDatabaseURL(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		setEnv      bool
		wantURL     string
		wantErr     bool
		wantErrCode string
	}{
		{
			name:        "returns error when DATABASE_URL not set",
			setEnv:      false,
			wantErr:     true,
			wantErrCode: "CONFIG_INVALID",
		},
		{
			name:        "returns error when DATABASE_URL is empty string",
			envValue:    "",
			setEnv:      true,
			wantErr:     true,
			wantErrCode: "CONFIG_INVALID",
		},
		{
			name:     "returns URL when DATABASE_URL is set",
			envValue: "postgres://localhost:5432/testdb",
			setEnv:   true,
			wantURL:  "postgres://localhost:5432/testdb",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("DATABASE_URL", tt.envValue)
			}

			url, err := getDatabaseURL()

			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				assert.Empty(t, url)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantURL, url)
			}
		})
	}
}
