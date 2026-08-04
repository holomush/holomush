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
		{"unicode name", "\u65E5\u672C\u8A9E\u306E\u540D\u524D", false, ""},
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

// TestValidateCharacterNameStillRejectsEverythingItRejectedBeforeThePipelineLanded
// pins the pre-existing validator behaviour against this phase's replacement of
// NormalizeCharacterName.
//
// 01-SPEC.md §6.1.5 is explicit that the letters-and-spaces shape rule stays as
// it is: the §6.1.1 pipeline's Cf stripping is an INDEPENDENT guarantee, not a
// substitute for the regex. These rows are the mechanical statement of "stays
// as it is" — they fail if a later plan relaxes the shape rule while wiring the
// pipeline in, which is exactly the moment the relaxation would be invisible.
func TestValidateCharacterNameStillRejectsEverythingItRejectedBeforeThePipelineLanded(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// The paired positive controls (PORTAL-10 rule 2). Without them, every
		// row below is satisfied by a validator that rejects all input — and a
		// non-Latin control additionally proves the rule is not ASCII-only.
		{name: "an ordinary Latin name", input: "Alaric"},
		{name: "an ordinary Latin name of two words", input: "John Smith"},
		{name: "a name written wholly in Cyrillic", input: "\u0418\u0432\u0430\u043D"},

		{name: "the empty string", input: "", wantErr: true, errMsg: "cannot be empty"},
		{
			name:    "more runes than the maximum",
			input:   strings.Repeat("a", MaxCharacterNameLength+1),
			wantErr: true,
			errMsg:  "at most",
		},
		{name: "a leading space", input: " Alaric", wantErr: true, errMsg: "leading or trailing spaces"},
		{name: "a trailing space", input: "Alaric ", wantErr: true, errMsg: "leading or trailing spaces"},
		{name: "consecutive spaces", input: "John  Smith", wantErr: true, errMsg: "consecutive spaces"},
		{name: "a digit", input: "Alaric2", wantErr: true, errMsg: "letters and spaces only"},
		{name: "an ASCII apostrophe", input: "O'Brien", wantErr: true, errMsg: "letters and spaces only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCharacterName(tt.input)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)

			var ve *ValidationError
			assert.ErrorAs(t, err, &ve, "the caller-facing error shape is unchanged")
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
		{"unicode description", "\u65E5\u672C\u8A9E\u306E\u8AAC\u660E", false, ""},
		{"newline allowed", "line1\nline2", false, ""},
		{"tab allowed", "column1\tcolumn2", false, ""},
		{"carriage return allowed", "line1\rline2", false, ""},
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
		{"invalid UTF-8 in alias", []string{"\xff\xfe"}, true, "must be valid UTF-8"},
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

	t.Run("non-serializable value rejected", func(t *testing.T) {
		// Channels cannot be JSON-serialized
		lockData := map[string]any{"channel": make(chan int)}
		err := ValidateLockData(lockData)
		require.Error(t, err, "lock data with non-serializable values should be rejected")
		assert.Contains(t, err.Error(), "not JSON-serializable")
	})

	t.Run("function value rejected", func(t *testing.T) {
		// Functions cannot be JSON-serialized
		lockData := map[string]any{"func": func() {}}
		err := ValidateLockData(lockData)
		require.Error(t, err, "lock data with function values should be rejected")
		assert.Contains(t, err.Error(), "not JSON-serializable")
	})
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
