// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatAllowedEntityTypes(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		expected string
	}{
		{
			name:     "empty",
			allowed:  nil,
			expected: "",
		},
		{
			name:     "single",
			allowed:  []string{"location"},
			expected: "'location'",
		},
		{
			name:     "pair",
			allowed:  []string{"location", "object"},
			expected: "'location' or 'object'",
		},
		{
			name:     "many",
			allowed:  []string{"a", "b", "c"},
			expected: "'a', 'b', 'c'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := formatAllowedEntityTypes(test.allowed)
			assert.Equal(t, test.expected, got)
		})
	}
}
