// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time interface checks: both theme types must satisfy Theme.
var (
	_ Theme = (*StarTheme)(nil)
	_ Theme = (*GemstoneElementTheme)(nil)
)

func TestTheme_Name(t *testing.T) {
	tests := []struct {
		name     string
		theme    Theme
		wantName string
	}{
		{"star theme", NewStarTheme(), "star"},
		{"gemstone element theme", NewGemstoneElementTheme(), "gemstone_element"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.theme.Name())
		})
	}
}

func TestTheme_Generate(t *testing.T) {
	tests := []struct {
		name            string
		theme           Theme
		wantFirstEmpty  bool
		wantSecondEmpty bool
	}{
		{"star theme returns non-empty first, empty second", NewStarTheme(), false, true},
		{"gemstone element theme returns non-empty first and second", NewGemstoneElementTheme(), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 10 {
				first, second := tt.theme.Generate()
				if tt.wantFirstEmpty {
					assert.Empty(t, first)
				} else {
					assert.NotEmpty(t, first)
				}
				if tt.wantSecondEmpty {
					assert.Empty(t, second)
				} else {
					assert.NotEmpty(t, second)
				}
			}
		})
	}
}

