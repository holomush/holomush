// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRegistry_Register(t *testing.T) {
	t.Run("registers a service and resolves it by name", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService", PluginName: "", PluginType: typeServerInternal}

		err := reg.Register(svc)
		require.NoError(t, err)

		resolved, err := reg.Resolve("holomush.world.v1.WorldService")
		require.NoError(t, err)
		assert.Equal(t, "holomush.world.v1.WorldService", resolved.Name)
	})

	t.Run("rejects duplicate service name", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService"}

		require.NoError(t, reg.Register(svc))
		err := reg.Register(svc)
		assert.Error(t, err)
	})
}

func TestServiceRegistry_Resolve(t *testing.T) {
	t.Run("returns error for unknown service", func(t *testing.T) {
		reg := NewServiceRegistry()
		_, err := reg.Resolve("holomush.fake.v1.FakeService")
		assert.Error(t, err)
	})
}

func TestServiceRegistry_Deregister(t *testing.T) {
	t.Run("removes a registered service", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService"}
		require.NoError(t, reg.Register(svc))

		err := reg.Deregister("holomush.world.v1.WorldService")
		require.NoError(t, err)

		_, err = reg.Resolve("holomush.world.v1.WorldService")
		assert.Error(t, err)
	})

	t.Run("returns error for unknown service", func(t *testing.T) {
		reg := NewServiceRegistry()
		err := reg.Deregister("holomush.fake.v1.FakeService")
		assert.Error(t, err)
	})
}

func TestServiceRegistry_List(t *testing.T) {
	t.Run("returns all registered services", func(t *testing.T) {
		reg := NewServiceRegistry()
		require.NoError(t, reg.Register(RegisteredService{Name: "svc-a"}))
		require.NoError(t, reg.Register(RegisteredService{Name: "svc-b"}))

		all := reg.List()
		assert.Len(t, all, 2)
	})
}
