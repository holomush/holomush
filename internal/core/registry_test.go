// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestVerbRegistryRegisterStoresVerbAndAllowsLookup(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type:     "say",
		Category: "communication",
		Format:   "speech",
		Label:    "says",
	})
	require.NoError(t, err)

	reg, ok := r.Lookup("say")
	assert.True(t, ok)
	assert.Equal(t, "communication", reg.Category)
	assert.Equal(t, "speech", reg.Format)
	assert.Equal(t, "says", reg.Label)
}

func TestVerbRegistryRegisterDuplicateTypeReturnsError(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech", Label: "says"})
	require.NoError(t, err)

	err = r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech", Label: "says"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestVerbRegistryLookupUnknownTypeReturnsFalse(t *testing.T) {
	r := NewVerbRegistry()
	_, ok := r.Lookup("nonexistent")
	assert.False(t, ok)
}

// TestVerbRegistryRegisterSpeechFormatWithoutLabelReturnsError enforces
// INV-GW-7: RenderingMetadata.label MUST be set when format == "speech".
func TestVerbRegistryRegisterSpeechFormatWithoutLabelReturnsError(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label is required")
}

func TestVerbRegistry_Register(t *testing.T) {
	tests := []struct {
		name string
		reg  VerbRegistration
		want string
	}{
		{"empty type", VerbRegistration{Category: "c", Format: "f"}, "type must not be empty"},
		{"empty category", VerbRegistration{Type: "t", Format: "f"}, "category must not be empty"},
		{"empty format", VerbRegistration{Type: "t", Category: "c"}, "format must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVerbRegistry()
			err := r.Register(tt.reg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestVerbRegistryConcurrentAccessIsSafe(t *testing.T) {
	r := NewVerbRegistry()
	var wg sync.WaitGroup
	errs := make(chan error, 50)

	// Concurrent writes (different types)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := r.Register(VerbRegistration{
				Type:     fmt.Sprintf("type_%d", n),
				Category: "communication",
				Format:   "action",
			}); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Lookup("type_a")
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected Register error: %v", err)
	}
}

func TestVerbRegistrySourceFieldPreservedThroughRegisterLookup(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type: "custom", Category: "communication", Format: "action", Source: "my-plugin",
	})
	require.NoError(t, err)

	reg, ok := r.Lookup("custom")
	require.True(t, ok)
	assert.Equal(t, "my-plugin", reg.Source)
}

func TestVerbRegistryUnregisterRemovesEntry(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type: "temp", Category: "system", Format: "notification", Source: "test",
	})
	require.NoError(t, err)

	removed := r.Unregister("temp")
	assert.True(t, removed)

	_, ok := r.Lookup("temp")
	assert.False(t, ok)
}

func TestVerbRegistryUnregisterNonexistentReturnsFalse(t *testing.T) {
	r := NewVerbRegistry()
	removed := r.Unregister("nonexistent")
	assert.False(t, removed)
}

func TestVerbRegistryUnregisterBySourceRemovesAllFromSource(t *testing.T) {
	r := NewVerbRegistry()
	require.NoError(t, r.Register(VerbRegistration{
		Type: "a", Category: "communication", Format: "action", Source: "plugin-x",
	}))
	require.NoError(t, r.Register(VerbRegistration{
		Type: "b", Category: "system", Format: "notification", Source: "plugin-x",
	}))
	require.NoError(t, r.Register(VerbRegistration{
		Type: "c", Category: "command", Format: "narrative", Source: "builtin",
	}))

	count := r.UnregisterBySource("plugin-x")
	assert.Equal(t, 2, count)

	_, ok := r.Lookup("a")
	assert.False(t, ok)
	_, ok = r.Lookup("b")
	assert.False(t, ok)
	_, ok = r.Lookup("c")
	assert.True(t, ok)
}

func TestVerbRegistryUnregisterBySourceUnknownReturnsZero(t *testing.T) {
	r := NewVerbRegistry()
	count := r.UnregisterBySource("nonexistent")
	assert.Equal(t, 0, count)
}

func TestRegisterBuiltinTypesRegistersAllKnownEventTypes(t *testing.T) {
	r := NewVerbRegistry()
	err := registerBuiltinTypes(r, "test")
	require.NoError(t, err)

	// registerBuiltinTypes registers ONLY host-owned event types per the
	// plugin-boundary discipline. Plugin-owned types (say/pose/whisper
	// from core-communication, object_* from core-objects) are registered
	// by the plugin loader from each plugin's manifest `verbs:` block —
	// see internal/plugin/manager.go.

	// Host-owned types are present:
	reg, ok := r.Lookup("command_error")
	require.True(t, ok)
	assert.Equal(t, "command", reg.Category)
	assert.Equal(t, "error", reg.Format)

	_, ok = r.Lookup("location_state")
	assert.True(t, ok)

	// Plugin-owned types are NOT registered by registerBuiltinTypes:
	_, ok = r.Lookup("core-communication:say")
	assert.False(t, ok, "say is plugin-owned and registered by the loader, not registerBuiltinTypes")
	_, ok = r.Lookup("core-objects:object_create")
	assert.False(t, ok, "object_create is plugin-owned and registered by the loader, not registerBuiltinTypes")
}

func TestRegisterWithSourceRecordsVersion(t *testing.T) {
	r := NewVerbRegistry()
	err := r.RegisterWithSource(VerbRegistration{
		Type:          "core-communication:say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "0.1.0")
	require.NoError(t, err)

	assert.Equal(t, "0.1.0", r.SourceVersion("core-communication"))
	assert.Equal(t, "", r.SourceVersion("nonexistent-plugin"))
}

func TestRegisterFallsBackToEmptyVersion(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type:          "core-objects:object_create",
		Category:      "state",
		Format:        "delta",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE,
		Source:        "core-objects",
	})
	require.NoError(t, err)
	assert.Equal(t, "", r.SourceVersion("core-objects"))
}

func TestRegisterBuiltinTypesDoesNotIncludeChannelTypes(t *testing.T) {
	r := NewVerbRegistry()
	err := registerBuiltinTypes(r, "test")
	require.NoError(t, err)

	channelTypes := []string{"channel_say", "channel_pose", "channel_system"}
	for _, ct := range channelTypes {
		_, ok := r.Lookup(ct)
		assert.False(t, ok, "builtin registry should not include %s", ct)
	}
}

func TestBootstrapVerbRegistryReturnsSeededRegistry(t *testing.T) {
	r, err := BootstrapVerbRegistry("0.4.2-test")
	require.NoError(t, err)

	// Builtins are registered.
	reg, ok := r.Lookup("arrive")
	require.True(t, ok)
	assert.Equal(t, "movement", reg.Category)
	assert.Equal(t, "builtin", reg.Source)

	// Source version uses the host- prefix.
	assert.Equal(t, "host-0.4.2-test", r.SourceVersion("builtin"))
}
