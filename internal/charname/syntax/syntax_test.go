// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package syntax_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname/syntax"
)

func TestValidateNameAcceptsLettersAndSingleSpacesWithinTheRuneBounds(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "single word", input: "Alaric"},
		{name: "two words", input: "John Smith"},
		{name: "three words", input: "John Paul Smith"},
		{name: "at the minimum rune length", input: "Al"},
		{name: "at the maximum rune length", input: "Abcdefghijklmnopqrstuvwxyzabcdef"},
		{name: "cyrillic letters", input: "Александрийскийгородзеленоград"},
		{name: "accented latin letters", input: "Éléonoreélisabethéléonore"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, syntax.ValidateName(tt.input))
		})
	}
}

func TestValidateNameRejectsNamesOutsideTheSyntacticRules(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{name: "the empty string", input: "", wantMsg: "cannot be empty"},
		{name: "a single rune is below the minimum", input: "A", wantMsg: "at least 2 characters"},
		{name: "thirty-three runes exceeds the maximum", input: "Abcdefghijklmnopqrstuvwxyzabcdefg", wantMsg: "at most 32 characters"},
		{name: "trailing digits", input: "Alaric123", wantMsg: "letters and spaces only"},
		{name: "a leading digit", input: "1Alaric", wantMsg: "letters and spaces only"},
		{name: "a digit in the middle", input: "Alar1c", wantMsg: "letters and spaces only"},
		{name: "punctuation", input: "Alaric!", wantMsg: "letters and spaces only"},
		{name: "an at sign", input: "Alaric@test", wantMsg: "letters and spaces only"},
		{name: "a hyphen", input: "John-Smith", wantMsg: "letters and spaces only"},
		{name: "an underscore", input: "John_Smith", wantMsg: "letters and spaces only"},
		{name: "an apostrophe", input: "O'Brien", wantMsg: "letters and spaces only"},
		{name: "a leading space", input: " Alaric", wantMsg: "leading or trailing spaces"},
		{name: "a trailing space", input: "Alaric ", wantMsg: "leading or trailing spaces"},
		{name: "a doubled space", input: "John  Smith", wantMsg: "consecutive spaces"},
		{name: "a tripled space", input: "John   Smith", wantMsg: "consecutive spaces"},
		{name: "only spaces", input: "   ", wantMsg: "leading or trailing spaces"},
		{name: "a NUL control character", input: "Alar\x00ic", wantMsg: "letters and spaces only"},
		{name: "a tab", input: "John\tSmith", wantMsg: "letters and spaces only"},
		{name: "a newline", input: "John\nSmith", wantMsg: "letters and spaces only"},
		{name: "invalid UTF-8 bytes", input: "\xff\xfe", wantMsg: "must be valid UTF-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syntax.ValidateName(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateNameReturnsATypedValidationErrorNamingTheNameField(t *testing.T) {
	err := syntax.ValidateName("Alaric123")
	require.Error(t, err)

	var verr *syntax.ValidationError
	require.True(t, errors.As(err, &verr), "the returned error is a *syntax.ValidationError")
	assert.Equal(t, "name", verr.Field)
	assert.Contains(t, verr.Message, "letters and spaces only")
}

func TestNameLengthBoundsAreExportedAsRuneCounts(t *testing.T) {
	assert.Equal(t, 2, syntax.MinNameLength)
	assert.Equal(t, 32, syntax.MaxNameLength)
}
