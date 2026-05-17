// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// TestEmitRegistry_RegisteredEmitTypes covers the registry's read surface
// across single/batch/duplicate/empty registration scenarios. Each case
// exercises the same call shape — register inputs, read sorted slice,
// assert — so a table form keeps the scenarios cheap to extend.
func TestEmitRegistry_RegisteredEmitTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(r *pluginsdk.EmitRegistry)
		expected []string // nil means assert NotNil + Empty (vs assert.Equal([], ...))
	}{
		{
			name:     "single RegisterEmitType returns a one-element sorted slice",
			register: func(r *pluginsdk.EmitRegistry) { r.RegisterEmitType("alpha") },
			expected: []string{"alpha"},
		},
		{
			name:     "batch RegisterEmitTypes returns a combined sorted set",
			register: func(r *pluginsdk.EmitRegistry) { r.RegisterEmitTypes([]string{"zeta", "alpha", "mike"}) },
			expected: []string{"alpha", "mike", "zeta"},
		},
		{
			name: "duplicate registration is idempotent",
			register: func(r *pluginsdk.EmitRegistry) {
				r.RegisterEmitType("foo")
				r.RegisterEmitType("foo")
				r.RegisterEmitTypes([]string{"foo", "bar"})
			},
			expected: []string{"bar", "foo"},
		},
		{
			name:     "empty registry returns a non-nil empty slice",
			register: func(_ *pluginsdk.EmitRegistry) {},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := pluginsdk.NewEmitRegistry()
			tt.register(r)

			got := r.RegisteredEmitTypes()
			if tt.expected == nil {
				require.NotNil(t, got)
				require.Empty(t, got)
				return
			}
			require.Equal(t, tt.expected, got)
		})
	}
}
