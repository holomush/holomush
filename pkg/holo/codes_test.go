// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This file contains tests for codes.go functionality that is not yet fully
// implemented. The build tag excludes it from compilation until the
// Fmt.Parse method is added.

package holo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFmt_Parse_StyleCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "bold code",
			input:    "%xhbold%xn",
			wantANSI: "\x1b[1mbold\x1b[0m",
		},
		{
			name:     "italic code",
			input:    "%xiitalic%xn",
			wantANSI: "\x1b[3mitalic\x1b[0m",
		},
		{
			name:     "underline code",
			input:    "%xuunderline%xn",
			wantANSI: "\x1b[4munderline\x1b[0m",
		},
		{
			name:     "dim code",
			input:    "%xddim%xn",
			wantANSI: "\x1b[2mdim\x1b[0m",
		},
		{
			name:     "reset only",
			input:    "%xn",
			wantANSI: "\x1b[0m",
		},
		{
			name:     "plain text no codes",
			input:    "hello world",
			wantANSI: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_ColorCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "red",
			input:    "%xrred%xn",
			wantANSI: "\x1b[31mred\x1b[0m",
		},
		{
			name:     "green",
			input:    "%xggreen%xn",
			wantANSI: "\x1b[32mgreen\x1b[0m",
		},
		{
			name:     "blue",
			input:    "%xbblue%xn",
			wantANSI: "\x1b[34mblue\x1b[0m",
		},
		{
			name:     "cyan",
			input:    "%xccyan%xn",
			wantANSI: "\x1b[36mcyan\x1b[0m",
		},
		{
			name:     "magenta",
			input:    "%xmmagenta%xn",
			wantANSI: "\x1b[35mmagenta\x1b[0m",
		},
		{
			name:     "yellow",
			input:    "%xyyellow%xn",
			wantANSI: "\x1b[33myellow\x1b[0m",
		},
		{
			name:     "white",
			input:    "%xwwhite%xn",
			wantANSI: "\x1b[37mwhite\x1b[0m",
		},
		{
			name:     "black",
			input:    "%xxblack%xn",
			wantANSI: "\x1b[30mblack\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_BrightColorCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "bright red",
			input:    "%xRbright red%xn",
			wantANSI: "\x1b[91mbright red\x1b[0m",
		},
		{
			name:     "bright green",
			input:    "%xGbright green%xn",
			wantANSI: "\x1b[92mbright green\x1b[0m",
		},
		{
			name:     "bright blue",
			input:    "%xBbright blue%xn",
			wantANSI: "\x1b[94mbright blue\x1b[0m",
		},
		{
			name:     "bright cyan",
			input:    "%xCbright cyan%xn",
			wantANSI: "\x1b[96mbright cyan\x1b[0m",
		},
		{
			name:     "bright magenta",
			input:    "%xMbright magenta%xn",
			wantANSI: "\x1b[95mbright magenta\x1b[0m",
		},
		{
			name:     "bright yellow",
			input:    "%xYbright yellow%xn",
			wantANSI: "\x1b[93mbright yellow\x1b[0m",
		},
		{
			name:     "bright white",
			input:    "%xWbright white%xn",
			wantANSI: "\x1b[97mbright white\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_WhitespaceCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "newline",
			input:    "line1%rline2",
			wantANSI: "line1\nline2",
		},
		{
			name:     "space",
			input:    "word1%bword2",
			wantANSI: "word1 word2",
		},
		{
			name:     "tab",
			input:    "col1%tcol2",
			wantANSI: "col1    col2",
		},
		{
			name:     "multiple newlines",
			input:    "a%r%rb",
			wantANSI: "a\n\nb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_256Color(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "color 0 (black)",
			input:    "%x000black%xn",
			wantANSI: "\x1b[38;5;0mblack\x1b[0m",
		},
		{
			name:     "color 196 (bright red)",
			input:    "%x196red%xn",
			wantANSI: "\x1b[38;5;196mred\x1b[0m",
		},
		{
			name:     "color 255 (white)",
			input:    "%x255white%xn",
			wantANSI: "\x1b[38;5;255mwhite\x1b[0m",
		},
		{
			name:     "color 042 (leading zero)",
			input:    "%x042green%xn",
			wantANSI: "\x1b[38;5;42mgreen\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_UnknownCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "unknown %x code preserved",
			input:    "%xqunknown",
			wantANSI: "%xqunknown",
		},
		{
			name:     "lone percent preserved",
			input:    "100% complete",
			wantANSI: "100% complete",
		},
		{
			name:     "double percent",
			input:    "100%% done",
			wantANSI: "100%% done",
		},
		{
			name:     "percent at end",
			input:    "100%",
			wantANSI: "100%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Parse(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Parse_Combined(t *testing.T) {
	input := "This is %xhbold%xn and %xrred%xn text with%ra newline"
	result := Fmt.Parse(input)
	rendered := result.RenderANSI()

	assert.Contains(t, rendered, "\x1b[1mbold\x1b[0m")
	assert.Contains(t, rendered, "\x1b[31mred\x1b[0m")
	assert.Contains(t, rendered, "\n")
}

func TestFmt_Parse_Empty(t *testing.T) {
	result := Fmt.Parse("")
	assert.Equal(t, "", result.RenderANSI())
}

func TestCodeToANSI_Coverage(t *testing.T) {
	// Ensure all expected codes are in the map
	expectedCodes := []string{"n", "h", "u", "i", "d", "r", "g", "b", "c", "m", "y", "w", "x", "R", "G", "B", "C", "M", "Y", "W"}

	for _, code := range expectedCodes {
		_, ok := codeToANSI[code]
		assert.True(t, ok, "expected code %q to be in codeToANSI map", code)
	}
}

func TestIsDigit(t *testing.T) {
	for b := byte('0'); b <= '9'; b++ {
		assert.True(t, isDigit(b), "expected %c to be a digit", b)
	}
	assert.False(t, isDigit('a'))
	assert.False(t, isDigit(' '))
	assert.False(t, isDigit('/'))
	assert.False(t, isDigit(':'))
}
