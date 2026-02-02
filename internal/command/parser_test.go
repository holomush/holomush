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
