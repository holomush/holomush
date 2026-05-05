// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugintest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPluginULIDFromNameIsDeterministic(t *testing.T) {
	a := PluginULIDFromName("core-scenes")
	b := PluginULIDFromName("core-scenes")
	assert.Equal(t, a, b, "PluginULIDFromName MUST be deterministic for same name")
}

func TestPluginULIDFromNameDistinguishesNames(t *testing.T) {
	a := PluginULIDFromName("plug-a")
	b := PluginULIDFromName("plug-b")
	assert.NotEqual(t, a, b, "different names MUST yield different ULIDs")
}

func TestStubRegistryResolvesByNameAndID(t *testing.T) {
	reg := NewStubRegistry("core-scenes", "plug-A")

	id, ok := reg.IDByName("core-scenes")
	assert.True(t, ok)
	assert.Equal(t, PluginULIDFromName("core-scenes"), id)

	name, ok := reg.NameByID(id)
	assert.True(t, ok)
	assert.Equal(t, "core-scenes", name)
}

func TestStubRegistryReturnsFalseForUnknownNames(t *testing.T) {
	reg := NewStubRegistry("core-scenes")
	_, ok := reg.IDByName("not-registered")
	assert.False(t, ok)
}
