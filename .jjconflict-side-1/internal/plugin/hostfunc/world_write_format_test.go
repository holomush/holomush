// Copyright 2026 HoloMUSH Contributors

package hostfunc

import "testing"

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
			if got := formatAllowedEntityTypes(test.allowed); got != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, got)
			}
		})
	}
}
