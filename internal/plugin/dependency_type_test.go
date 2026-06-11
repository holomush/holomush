// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDependencyUnmarshalsBareStringAsService(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`holomush.scene.v1.SceneService`), &d))
	assert.Equal(t, DependencyService, d.Kind)
	assert.Equal(t, "holomush.scene.v1.SceneService", d.Name)
	assert.False(t, d.Optional)
}

func TestDependencyUnmarshalsCapabilityEntry(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`{capability: world.query}`), &d))
	assert.Equal(t, DependencyCapability, d.Kind)
	assert.Equal(t, "world.query", d.Name)
}

func TestDependencyUnmarshalsServiceEntryWithAttributes(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`{service: holomush.scene.v1.SceneService, version: ">=1.0.0", optional: true}`), &d))
	assert.Equal(t, DependencyService, d.Kind)
	assert.Equal(t, "holomush.scene.v1.SceneService", d.Name)
	assert.Equal(t, ">=1.0.0", d.Version)
	assert.True(t, d.Optional)
}

func TestDependencyRejectsEntryWithBothKinds(t *testing.T) {
	var d Dependency
	err := yaml.Unmarshal([]byte(`{capability: x, service: y}`), &d)
	assert.Error(t, err)
}

func TestDependencyRejectsEntryWithNeitherKind(t *testing.T) {
	var d Dependency
	err := yaml.Unmarshal([]byte(`{version: ">=1.0.0"}`), &d)
	assert.Error(t, err)
}

func TestRequireServicesConstructsServiceDeps(t *testing.T) {
	deps := RequireServices("a", "b")
	require.Len(t, deps, 2)
	assert.Equal(t, DependencyService, deps[0].Kind)
	assert.Equal(t, "a", deps[0].Name)
}
