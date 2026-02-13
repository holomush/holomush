// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

func TestEnvironmentProvider_Namespace(t *testing.T) {
	provider := NewEnvironmentProvider(nil)
	assert.Equal(t, "env", provider.Namespace())
}

func TestEnvironmentProvider_Resolve(t *testing.T) {
	fixedTime := time.Date(2026, 2, 12, 14, 30, 0, 0, time.UTC) // Thursday 14:30

	tests := []struct {
		name     string
		clock    func() time.Time
		expected map[string]any
	}{
		{
			name:  "fixed time returns expected attributes",
			clock: func() time.Time { return fixedTime },
			expected: map[string]any{
				"time":        "2026-02-12T14:30:00Z",
				"hour":        float64(14),
				"minute":      float64(30),
				"day_of_week": "thursday",
				"maintenance": false,
			},
		},
		{
			name:  "different time different values",
			clock: func() time.Time { return time.Date(2026, 1, 5, 9, 15, 0, 0, time.UTC) }, // Monday 9:15
			expected: map[string]any{
				"time":        "2026-01-05T09:15:00Z",
				"hour":        float64(9),
				"minute":      float64(15),
				"day_of_week": "monday",
				"maintenance": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewEnvironmentProvider(tt.clock)
			attrs, err := provider.Resolve(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expected, attrs)
		})
	}
}

func TestEnvironmentProvider_Schema(t *testing.T) {
	provider := NewEnvironmentProvider(nil)
	schema := provider.Schema()

	expected := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"time":        types.AttrTypeString,
			"hour":        types.AttrTypeFloat,
			"minute":      types.AttrTypeFloat,
			"day_of_week": types.AttrTypeString,
			"maintenance": types.AttrTypeBool,
		},
	}

	assert.Equal(t, expected, schema)
}

func TestEnvironmentProvider_DefaultClock(t *testing.T) {
	provider := NewEnvironmentProvider(nil)
	attrs, err := provider.Resolve(context.Background())
	require.NoError(t, err)

	// Should have all expected keys
	assert.Contains(t, attrs, "time")
	assert.Contains(t, attrs, "hour")
	assert.Contains(t, attrs, "minute")
	assert.Contains(t, attrs, "day_of_week")
	assert.Contains(t, attrs, "maintenance")

	// Validate types
	assert.IsType(t, "", attrs["time"])
	assert.IsType(t, float64(0), attrs["hour"])
	assert.IsType(t, float64(0), attrs["minute"])
	assert.IsType(t, "", attrs["day_of_week"])
	assert.IsType(t, false, attrs["maintenance"])
}
