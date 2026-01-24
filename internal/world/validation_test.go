// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid name", "Town Square", false, ""},
		{"empty name", "", true, "cannot be empty"},
		{"name too long", strings.Repeat("a", MaxNameLength+1), true, "exceeds maximum length"},
		{"max length name", strings.Repeat("a", MaxNameLength), false, ""},
		{"unicode name", "日本語の名前", false, ""},
		{"invalid UTF-8 bytes", "\xff\xfe", true, "must be valid UTF-8"},
		{"control char", "name\x00with null", true, "cannot contain control characters"},
		{"newline not allowed", "name\nwith newline", true, "cannot contain control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				var ve *ValidationError
				assert.ErrorAs(t, err, &ve)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDescription(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid description", "A beautiful town square.", false, ""},
		{"empty description", "", false, ""},
		{"description too long", strings.Repeat("a", MaxDescriptionLength+1), true, "exceeds maximum length"},
		{"max length description", strings.Repeat("a", MaxDescriptionLength), false, ""},
		{"unicode description", "日本語の説明", false, ""},
		{"newline allowed", "line1\nline2", false, ""},
		{"tab allowed", "column1\tcolumn2", false, ""},
		{"invalid UTF-8 bytes", "\xff\xfe", true, "must be valid UTF-8"},
		{"control char", "desc\x00with null", true, "cannot contain control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDescription(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAliases(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantErr bool
		errMsg  string
	}{
		{"valid aliases", []string{"n", "north"}, false, ""},
		{"empty list", []string{}, false, ""},
		{"nil list", nil, false, ""},
		{"too many aliases", make([]string, MaxAliasCount+1), true, "exceeds maximum count"},
		{"empty alias", []string{"n", ""}, true, "cannot be empty"},
		{"alias too long", []string{strings.Repeat("a", MaxAliasLength+1)}, true, "exceeds maximum length"},
		{"control char in alias", []string{"n\x00"}, true, "cannot contain control characters"},
	}

	// Fill the "too many aliases" test case with valid values
	for i := range tests {
		if tests[i].name == "too many aliases" {
			for j := range MaxAliasCount + 1 {
				tests[i].input[j] = "alias"
			}
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAliases(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateVisibleTo(t *testing.T) {
	id1 := ulid.Make()
	id2 := ulid.Make()

	tests := []struct {
		name    string
		input   []ulid.ULID
		wantErr bool
		errMsg  string
	}{
		{"valid list", []ulid.ULID{id1, id2}, false, ""},
		{"empty list", []ulid.ULID{}, false, ""},
		{"nil list", nil, false, ""},
		{"duplicate ID", []ulid.ULID{id1, id1}, true, "duplicate ID"},
	}

	// Create a too-large list
	t.Run("too many IDs", func(t *testing.T) {
		largeList := make([]ulid.ULID, MaxVisibleToCount+1)
		for i := range largeList {
			largeList[i] = ulid.Make()
		}
		err := ValidateVisibleTo(largeList)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum count")
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVisibleTo(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateLockData(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		errMsg  string
	}{
		{"valid lock data", map[string]any{"key_id": "abc123"}, false, ""},
		{"nil lock data", nil, false, ""},
		{"empty lock data", map[string]any{}, false, ""},
		{"empty key", map[string]any{"": "value"}, true, "key cannot be empty"},
		{"invalid key", map[string]any{"123invalid": "value"}, true, "not a valid identifier"},
		{"key with space", map[string]any{"has space": "value"}, true, "not a valid identifier"},
	}

	// Create too many keys
	t.Run("too many keys", func(t *testing.T) {
		largeMap := make(map[string]any, MaxLockDataKeys+1)
		for i := range MaxLockDataKeys + 1 {
			largeMap["key"+string(rune('a'+i%26))+string(rune('0'+i/26))] = "value"
		}
		err := ValidateLockData(largeMap)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum key count")
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLockData(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"key", true},
		{"key_id", true},
		{"_private", true},
		{"key123", true},
		{"Key", true},
		{"", false},
		{"123key", false},
		{"key-id", false},
		{"key id", false},
		{"key.id", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidIdentifier(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
