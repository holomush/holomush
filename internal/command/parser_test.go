// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantArgs string
		wantErr  bool
	}{
		{
			name:     "simple command",
			input:    "look",
			wantCmd:  "look",
			wantArgs: "",
		},
		{
			name:     "command with args",
			input:    "say hello world",
			wantCmd:  "say",
			wantArgs: "hello world",
		},
		{
			name:     "command with leading whitespace",
			input:    "   look",
			wantCmd:  "look",
			wantArgs: "",
		},
		{
			name:     "command with trailing whitespace",
			input:    "look   ",
			wantCmd:  "look",
			wantArgs: "",
		},
		{
			name:     "preserves internal arg whitespace",
			input:    "say   hello    world",
			wantCmd:  "say",
			wantArgs: "hello    world",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:     "command with tab separator",
			input:    "say\thello",
			wantCmd:  "say",
			wantArgs: "hello",
		},
		{
			name:     "tab characters in args preserved",
			input:    "say hello\tworld",
			wantCmd:  "say",
			wantArgs: "hello\tworld",
		},
		{
			name:     "mixed whitespace separator",
			input:    "say \t hello",
			wantCmd:  "say",
			wantArgs: "hello",
		},
		// Unicode input tests
		{
			name:     "unicode arguments (Chinese)",
			input:    "say 你好世界",
			wantCmd:  "say",
			wantArgs: "你好世界",
		},
		{
			name:     "emoji arguments",
			input:    "say Hello! 👋",
			wantCmd:  "say",
			wantArgs: "Hello! 👋",
		},
		{
			name:     "unicode in quoted context",
			input:    `say "café résumé"`,
			wantCmd:  "say",
			wantArgs: `"café résumé"`,
		},
		{
			name:     "mixed ASCII and unicode",
			input:    "say Hello 世界",
			wantCmd:  "say",
			wantArgs: "Hello 世界",
		},
		{
			name:     "unicode command name",
			input:    "日本語 argument",
			wantCmd:  "日本語",
			wantArgs: "argument",
		},
		{
			name:     "multi-byte emoji sequence",
			input:    "emote 👨‍👩‍👧‍👦 waves",
			wantCmd:  "emote",
			wantArgs: "👨‍👩‍👧‍👦 waves",
		},
		{
			name:     "accented characters",
			input:    "whisper naïve façade",
			wantCmd:  "whisper",
			wantArgs: "naïve façade",
		},
		{
			name:     "right-to-left script (Arabic)",
			input:    "say مرحبا",
			wantCmd:  "say",
			wantArgs: "مرحبا",
		},
		{
			name:     "unicode whitespace only args trimmed",
			input:    "look   ",
			wantCmd:  "look",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCmd, parsed.Name)
			assert.Equal(t, tt.wantArgs, parsed.Args)
			assert.Equal(t, tt.input, parsed.Raw)
		})
	}
}
