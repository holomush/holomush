// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntityRef(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantType    string
		wantID      string
		wantErr     bool
		wantErrCode string
	}{
		// Subject prefixes
		{
			name:     "character subject",
			input:    "character:01ABC",
			wantType: "character",
			wantID:   "01ABC",
		},
		{
			name:     "plugin subject",
			input:    "plugin:echo-bot",
			wantType: "plugin",
			wantID:   "echo-bot",
		},
		{
			name:     "system subject (no ID)",
			input:    "system",
			wantType: "system",
			wantID:   "",
		},
		{
			name:     "session subject",
			input:    "session:abc123",
			wantType: "session",
			wantID:   "abc123",
		},

		// Resource prefixes
		{
			name:     "location resource",
			input:    "location:01XYZ",
			wantType: "location",
			wantID:   "01XYZ",
		},
		{
			name:     "object resource",
			input:    "object:01DEF",
			wantType: "object",
			wantID:   "01DEF",
		},
		{
			name:     "command resource",
			input:    "command:dig",
			wantType: "command",
			wantID:   "dig",
		},
		{
			name:     "property resource",
			input:    "property:01GHI",
			wantType: "property",
			wantID:   "01GHI",
		},
		{
			name:     "stream resource with compound ID",
			input:    "stream:location:01XYZ",
			wantType: "stream",
			wantID:   "location:01XYZ",
		},
		{
			name:     "exit resource",
			input:    "exit:01JKL",
			wantType: "exit",
			wantID:   "01JKL",
		},
		{
			name:     "scene resource",
			input:    "scene:01MNO",
			wantType: "scene",
			wantID:   "01MNO",
		},

		// Error cases
		{
			name:        "empty string",
			input:       "",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "unknown prefix",
			input:       "bogus:01ABC",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "legacy char prefix",
			input:       "char:01ABC",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "empty ID after character prefix",
			input:       "character:",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "empty ID after location prefix",
			input:       "location:",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeName, id, err := access.ParseEntityRef(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, typeName)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestSubjectPrefixConstants(t *testing.T) {
	assert.Equal(t, "character:", access.SubjectCharacter)
	assert.Equal(t, "plugin:", access.SubjectPlugin)
	assert.Equal(t, "system", access.SubjectSystem)
	assert.Equal(t, "session:", access.SubjectSession)
}

func TestResourcePrefixConstants(t *testing.T) {
	assert.Equal(t, "location:", access.ResourceLocation)
	assert.Equal(t, "object:", access.ResourceObject)
	assert.Equal(t, "command:", access.ResourceCommand)
	assert.Equal(t, "property:", access.ResourceProperty)
	assert.Equal(t, "stream:", access.ResourceStream)
	assert.Equal(t, "exit:", access.ResourceExit)
	assert.Equal(t, "scene:", access.ResourceScene)
}

func TestSessionErrorCodeConstants(t *testing.T) {
	assert.Equal(t, "infra:session-invalid", access.ErrCodeSessionInvalid)
	assert.Equal(t, "infra:session-store-error", access.ErrCodeSessionStoreError)
}

func TestCharacterSubject(t *testing.T) {
	tests := []struct {
		name     string
		charID   string
		expected string
	}{
		{
			name:     "ULID string",
			charID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "character:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			charID:   "test-id",
			expected: "character:test-id",
		},
		{
			name:     "empty ID",
			charID:   "",
			expected: "character:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.CharacterSubject(tt.charID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
