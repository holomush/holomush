// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCommandName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple lowercase", "look", false},
		{"with at prefix", "@create", true}, // @ at start is invalid - must start with letter
		{"at in middle", "a@create", false},
		{"with plus prefix", "+who", true}, // + at start is invalid - must start with letter
		{"plus in middle", "a+who", false},
		{"with underscore", "my_cmd", false},
		{"with question mark", "say?", false},
		{"with exclamation", "say!", false},
		{"max length 20", "abcdefghijklmnopqrst", false},
		{"too long 21", "abcdefghijklmnopqrstu", true},
		{"starts with digit", "123go", true},
		{"starts with star", "*star", true},
		{"empty", "", true},
		{"only spaces", "   ", true},
		{"single letter", "l", false},
		{"with hash", "a#cmd", false},
		{"with dollar", "a$cmd", false},
		{"with percent", "a%cmd", false},
		{"with caret", "a^cmd", false},
		{"with hyphen", "a-cmd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommandName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAliasName(t *testing.T) {
	// Aliases follow same rules as commands
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"single letter", "l", false},
		{"lowercase", "look", false},
		{"starts with digit", "1look", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAliasName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
