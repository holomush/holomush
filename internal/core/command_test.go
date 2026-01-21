// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
		wantArg string
	}{
		{"connect", "connect user pass", "connect", "user pass"},
		{"say", "say hello world", "say", "hello world"},
		{"look", "look", "look", ""},
		{"pose", "pose waves", "pose", "waves"},
		{"quit", "quit", "quit", ""},
		{"empty", "", "", ""},
		{"whitespace", "  say  hello  ", "say", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, arg := ParseCommand(tt.input)
			assert.Equal(t, tt.wantCmd, cmd)
			assert.Equal(t, tt.wantArg, arg)
		})
	}
}
