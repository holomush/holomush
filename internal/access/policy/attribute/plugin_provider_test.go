// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

type mockPluginRegistry struct {
	loaded map[string]bool
}

func (m *mockPluginRegistry) IsPluginLoaded(name string) bool {
	return m.loaded[name]
}

func TestPluginProvider_Namespace(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	assert.Equal(t, "plugin", p.Namespace())
}

func TestPluginProvider_ResolveSubject(t *testing.T) {
	registry := &mockPluginRegistry{loaded: map[string]bool{"echo-bot": true}}
	p := NewPluginProvider(registry)

	tests := []struct {
		name        string
		subjectID   string
		expectAttrs map[string]any
		expectNil   bool
	}{
		{
			name:        "loaded plugin",
			subjectID:   "echo-bot",
			expectAttrs: map[string]any{"name": "echo-bot"},
		},
		{
			name:      "unloaded plugin returns nil",
			subjectID: "unknown-plugin",
			expectNil: true,
		},
		{
			name:      "empty ID returns nil",
			subjectID: "",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := p.ResolveSubject(context.Background(), tt.subjectID)
			require.NoError(t, err)
			if tt.expectNil {
				assert.Nil(t, attrs)
			} else {
				assert.Equal(t, tt.expectAttrs, attrs)
			}
		})
	}
}

func TestPluginProvider_NilRegistry_DeniesAll(t *testing.T) {
	p := NewPluginProvider(nil)
	attrs, err := p.ResolveSubject(context.Background(), "any-plugin")
	require.NoError(t, err)
	assert.Nil(t, attrs, "nil registry must deny attribute resolution (fail-closed)")
}

func TestPluginProvider_ResolveResource(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	attrs, err := p.ResolveResource(context.Background(), "anything")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestPluginProvider_Schema(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	schema := p.Schema()
	require.NotNil(t, schema)
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
}
