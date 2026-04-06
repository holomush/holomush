// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestCapabilityRegistry(t *testing.T) {
	t.Run("registers and retrieves a capability module by service name", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		module := &stubCapability{name: "test"}
		reg.Register("holomush.test.v1.TestService", module)

		found := reg.Get("holomush.test.v1.TestService")
		require.NotNil(t, found)
		assert.Equal(t, "test", found.Namespace())
	})

	t.Run("returns nil for unregistered service", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		assert.Nil(t, reg.Get("missing"))
	})

	t.Run("injects only required capabilities into Lua state", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		reg.Register("svc-a", &stubCapability{name: "a"})
		reg.Register("svc-b", &stubCapability{name: "b"})

		L := lua.NewState()
		defer L.Close()

		// Only require svc-a
		reg.InjectRequired(L, []string{"svc-a"}, "test-plugin")

		// svc-a should have set a global
		assert.NotEqual(t, lua.LNil, L.GetGlobal("a"))
		// svc-b should NOT be injected
		assert.Equal(t, lua.LNil, L.GetGlobal("b"))
	})

	t.Run("skips unknown services without error", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		reg.Register("svc-a", &stubCapability{name: "a"})

		L := lua.NewState()
		defer L.Close()

		reg.InjectRequired(L, []string{"nonexistent"}, "test-plugin")

		assert.Equal(t, lua.LNil, L.GetGlobal("a"))
		assert.Equal(t, lua.LNil, L.GetGlobal("nonexistent"))
	})

	t.Run("lists all registered service names", func(t *testing.T) {
		reg := NewCapabilityRegistry()
		reg.Register("svc-a", &stubCapability{name: "a"})
		reg.Register("svc-b", &stubCapability{name: "b"})

		names := reg.List()
		assert.Len(t, names, 2)
		assert.Contains(t, names, "svc-a")
		assert.Contains(t, names, "svc-b")
	})
}

type stubCapability struct {
	name string
}

func (s *stubCapability) Namespace() string { return s.name }

func (s *stubCapability) Register(L *lua.LState, _ string) {
	tbl := L.NewTable()
	L.SetGlobal(s.name, tbl)
}
