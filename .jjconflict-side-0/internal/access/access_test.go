// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/stretchr/testify/assert"
)

func TestParseSubject(t *testing.T) {
	tests := []struct {
		name           string
		subject        string
		expectedPrefix string
		expectedID     string
	}{
		{"character", "character:01ABC", "character", "01ABC"},
		{"session", "session:01XYZ", "session", "01XYZ"},
		{"plugin", "plugin:echo-bot", "plugin", "echo-bot"},
		{"system", "system", "system", ""},
		{"no prefix", "invalid", "", "invalid"},
		{"empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, id := access.ParseSubject(tt.subject)
			assert.Equal(t, tt.expectedPrefix, prefix)
			assert.Equal(t, tt.expectedID, id)
		})
	}
}
