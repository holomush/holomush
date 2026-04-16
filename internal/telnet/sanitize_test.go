// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeTelnetOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "passes plain ASCII text through unchanged",
			input:    "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "preserves newline, carriage return, and tab",
			input:    "line1\nline2\r\n\tindented",
			expected: "line1\nline2\r\n\tindented",
		},
		{
			name:     "preserves UTF-8 multibyte characters",
			input:    "caf\u00e9 \u2603 \U0001F600",
			expected: "caf\u00e9 \u2603 \U0001F600",
		},
		{
			name:     "strips ANSI CSI escape sequence with final letter",
			input:    "red \x1b[31mtext\x1b[0m reset",
			expected: "red text reset",
		},
		{
			name:     "strips ANSI CSI cursor movement sequence",
			input:    "before\x1b[2J\x1b[Hafter",
			expected: "beforeafter",
		},
		{
			name:     "strips ANSI OSC sequence terminated by BEL",
			input:    "title\x1b]0;evil\x07rest",
			expected: "titlerest",
		},
		{
			name:     "strips ANSI OSC sequence terminated by ST",
			input:    "title\x1b]0;evil\x1b\\rest",
			expected: "titlerest",
		},
		{
			name:     "strips two-byte ESC sequence (ESC + single byte)",
			input:    "foo\x1bMbar",
			expected: "foobar",
		},
		{
			name:     "strips C0 control characters except whitespace",
			input:    "a\x00b\x01c\x07d\x08e\x0bf",
			expected: "abcdef",
		},
		{
			name:     "strips DEL character",
			input:    "foo\x7fbar",
			expected: "foobar",
		},
		{
			name:     "strips C1 control characters",
			input:    "foo\u0080\u0085\u009fbar",
			expected: "foobar",
		},
		{
			name:     "strips CSI via 8-bit C1 introducer",
			input:    "foo\u009b31mbar",
			expected: "foobar",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "handles CSI with parameter and intermediate bytes",
			input:    "x\x1b[1;31;40mhello\x1b[mend",
			expected: "xhelloend",
		},
		{
			name:     "strips lone ESC at end of string",
			input:    "trailing\x1b",
			expected: "trailing",
		},
		{
			name:     "strips unterminated CSI at end of string",
			input:    "unterm\x1b[31",
			expected: "unterm",
		},
		{
			name:     "strips unterminated OSC at end of string",
			input:    "unterm\x1b]0;title",
			expected: "unterm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeTelnetOutput(tt.input))
		})
	}
}
