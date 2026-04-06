// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisteredService(t *testing.T) {
	t.Run("stores service metadata alongside connection", func(t *testing.T) {
		svc := RegisteredService{
			Name:       "holomush.scene.v1.SceneService",
			PluginName: "core-scenes",
			PluginType: TypeBinary,
		}
		assert.Equal(t, "holomush.scene.v1.SceneService", svc.Name)
		assert.Equal(t, "core-scenes", svc.PluginName)
		assert.Equal(t, TypeBinary, svc.PluginType)
	})

	t.Run("server-internal service has empty plugin name", func(t *testing.T) {
		svc := RegisteredService{
			Name:       "holomush.world.v1.WorldService",
			PluginName: "",
			PluginType: typeServerInternal,
		}
		assert.True(t, svc.IsServerInternal())
	})

	t.Run("binary plugin service is not server-internal", func(t *testing.T) {
		svc := RegisteredService{
			Name:       "holomush.scene.v1.SceneService",
			PluginType: TypeBinary,
		}
		assert.False(t, svc.IsServerInternal())
	})
}
