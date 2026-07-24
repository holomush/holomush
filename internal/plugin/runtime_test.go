// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// runtimeStubHost is a minimal Host for exercising PluginRuntime's delivery
// and lookup paths. It deliberately implements nothing optional, so
// capabilitiesFor's empty-capability path is the one under test.
type runtimeStubHost struct {
	deliveredEvents   []string
	deliveredCommands []string
}

func (h *runtimeStubHost) Load(context.Context, *plugins.Manifest, string) error { return nil }
func (h *runtimeStubHost) Unload(context.Context, string) error                  { return nil }

func (h *runtimeStubHost) DeliverEvent(_ context.Context, name string, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.deliveredEvents = append(h.deliveredEvents, name)
	return nil, nil
}

func (h *runtimeStubHost) DeliverCommand(_ context.Context, name string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.deliveredCommands = append(h.deliveredCommands, name)
	return &pluginsdk.CommandResponse{}, nil
}

func (h *runtimeStubHost) QuerySessionStreams(context.Context, string, plugins.SessionStreamsRequest) ([]string, error) {
	return nil, nil
}

func (h *runtimeStubHost) Plugins() []string                          { return nil }
func (h *runtimeStubHost) PluginEmitRegistry(string) ([]string, bool) { return nil, false }
func (h *runtimeStubHost) Close(context.Context) error                { return nil }

// --- Behavior 1: D-02 / SC2 proof ------------------------------------------

// TestNewPluginRuntimeIsConstructibleWithoutAManager is the SC2 proof for this
// unit: PluginRuntime is built from package plugins_test with no *Manager, no
// plugins.NewManager call (so ErrMissingVerbRegistry / INV-EVENTBUS-11 is never
// in play) and no integration harness.
func TestNewPluginRuntimeIsConstructibleWithoutAManager(t *testing.T) {
	rt := plugins.NewPluginRuntime()
	require.NotNil(t, rt)

	host := &runtimeStubHost{}
	manifest := &plugins.Manifest{Name: "solo", Version: "1.0.0", Type: plugins.TypeLua}
	rt.CommitLoaded(&plugins.DiscoveredPlugin{Manifest: manifest}, host)

	assert.True(t, rt.IsPluginLoaded("solo"))
	dp, ok := rt.GetLoadedPlugin("solo")
	require.True(t, ok)
	assert.Equal(t, "solo", dp.Manifest.Name)
	assert.Equal(t, []string{"solo"}, rt.ListPlugins())
}

func TestPluginRuntimeReportsUnknownPluginAsNotLoaded(t *testing.T) {
	rt := plugins.NewPluginRuntime()

	assert.False(t, rt.IsPluginLoaded("absent"))
	dp, ok := rt.GetLoadedPlugin("absent")
	assert.False(t, ok)
	assert.Nil(t, dp)
}

// --- Behavior 2: lookupManifest fallback order ------------------------------

// TestPluginRuntimeLookupManifestFallsBackFromLoadedToInflight asserts all
// three branches in ONE test body so a later edit cannot half-satisfy the
// loaded-then-inflight fallback order (T-8-20). Inverting or dropping the
// fallback would resolve a partially-loaded plugin's manifest differently,
// changing which crypto gates apply mid-load.
func TestPluginRuntimeLookupManifestFallsBackFromLoadedToInflight(t *testing.T) {
	rt := plugins.NewPluginRuntime()

	loadedManifest := &plugins.Manifest{Name: "committed", Version: "1.0.0", Type: plugins.TypeLua}
	rt.CommitLoaded(&plugins.DiscoveredPlugin{Manifest: loadedManifest}, &runtimeStubHost{})

	inflightManifest := &plugins.Manifest{Name: "loading", Version: "1.0.0", Type: plugins.TypeLua}
	require.NoError(t, rt.ClaimInflight(&plugins.DiscoveredPlugin{Manifest: inflightManifest}))

	assert.Same(t, loadedManifest, rt.TestLookupManifest("committed"),
		"a name in loaded MUST resolve from loaded")
	assert.Same(t, inflightManifest, rt.TestLookupManifest("loading"),
		"a name present ONLY in inflight MUST still resolve — the fallback")
	assert.Nil(t, rt.TestLookupManifest("neither"),
		"an unknown name MUST resolve to nil")
}

// --- Behavior 3: PluginRequestsDecryption ----------------------------------

func TestPluginRuntimePluginRequestsDecryption(t *testing.T) {
	// declaring requests decryption for exactly one qualified ref; everything
	// else routed at this gate MUST deny.
	declaring := &plugins.Manifest{
		Name:    "mod-filter",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{
				{
					Subjects:           []string{"events.>"},
					RequestsDecryption: []string{"core-comm:whisper"},
				},
			},
		},
	}
	noCryptoSection := &plugins.Manifest{
		Name:    "no-crypto-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
	}

	tests := []struct {
		name      string
		manifest  *plugins.Manifest
		plugin    string
		eventType string
		want      bool
	}{
		{
			name:      "permits a qualified ref the manifest declares",
			manifest:  declaring,
			plugin:    "mod-filter",
			eventType: "core-comm:whisper",
			want:      true,
		},
		{
			name:      "denies a qualified ref the manifest does not declare",
			manifest:  declaring,
			plugin:    "mod-filter",
			eventType: "core-comm:undeclared",
			want:      false,
		},
		{
			name:      "denies an unknown plugin name",
			manifest:  declaring,
			plugin:    "nonexistent-plugin",
			eventType: "core-comm:whisper",
			want:      false,
		},
		{
			name:      "denies every ref when the manifest has no crypto section",
			manifest:  noCryptoSection,
			plugin:    "no-crypto-plugin",
			eventType: "anything:event",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newTestRuntimeWithManifest(t, tt.manifest)

			assert.Equal(t, tt.want, rt.PluginRequestsDecryption(tt.plugin, tt.eventType))
		})
	}
}

// --- Behavior 4: PluginCanReadBack -----------------------------------------

func TestPluginRuntimePluginCanReadBackHonoursTheReadbackFlag(t *testing.T) {
	rt := newTestRuntimeWithManifest(t, &plugins.Manifest{
		Name:    "core-comm",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "whisper", Readback: true},
				{EventType: "shout", Readback: false},
			},
		},
	})

	assert.True(t, rt.PluginCanReadBack("core-comm", "core-comm:whisper"),
		"readback: true MUST authorize read-back")
	assert.False(t, rt.PluginCanReadBack("core-comm", "core-comm:shout"),
		"readback unset MUST deny read-back")
	assert.False(t, rt.PluginCanReadBack("core-comm", "core-comm:unknown"),
		"an undeclared event type MUST deny read-back")
}

// --- Behavior 5: nil-receiver fail-closed guards (from 08-02) ---------------

// TestPluginRuntimeCryptoGatesReturnFalseOnNilReceiver pins, at the RELOCATED
// receiver, the fail-closed contract 08-02 proved RED-first on *Manager.
//
// Pre-split, `&Manager{}` (a fixture bypassing NewManager) read a nil `loaded`
// map and returned false. Post-split its `runtime` field is nil, so without
// these guards the same fixture panics inside r.mu.RLock() — converting a
// denial into a crash on the decrypt path. The guards restore the exact
// pre-split observable behavior; they are not new defensive code.
func TestPluginRuntimeCryptoGatesReturnFalseOnNilReceiver(t *testing.T) {
	var rt *plugins.PluginRuntime

	require.NotPanics(t, func() {
		assert.False(t, rt.PluginRequestsDecryption("any-plugin", "any-event"),
			"nil runtime must deny (fail-closed), not panic")
	})
	require.NotPanics(t, func() {
		assert.False(t, rt.PluginCanReadBack("any-plugin", "any-event"),
			"nil runtime must deny (fail-closed), not panic")
	})
}

// --- Behavior 6: delivery to an unregistered plugin -------------------------

func TestPluginRuntimeDeliveryToUnregisteredPluginErrorsWithoutPanicking(t *testing.T) {
	rt := plugins.NewPluginRuntime()
	ctx := context.Background()

	require.NotPanics(t, func() {
		emits, err := rt.DeliverEvent(ctx, "ghost", pluginsdk.Event{})
		assert.Error(t, err)
		assert.Nil(t, emits)
	})
	require.NotPanics(t, func() {
		resp, err := rt.DeliverCommand(ctx, "ghost", pluginsdk.CommandRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestPluginRuntimeDeliversToTheHostOwningTheNamedPlugin(t *testing.T) {
	rt := plugins.NewPluginRuntime()
	host := &runtimeStubHost{}
	rt.CommitLoaded(&plugins.DiscoveredPlugin{
		Manifest: &plugins.Manifest{Name: "routed", Version: "1.0.0", Type: plugins.TypeLua},
	}, host)

	_, err := rt.DeliverEvent(context.Background(), "routed", pluginsdk.Event{})
	require.NoError(t, err)
	_, err = rt.DeliverCommand(context.Background(), "routed", pluginsdk.CommandRequest{})
	require.NoError(t, err)

	assert.Equal(t, []string{"routed"}, host.deliveredEvents)
	assert.Equal(t, []string{"routed"}, host.deliveredCommands)
}

// newTestRuntimeWithManifest returns a PluginRuntime holding the given
// manifest in its loaded map — the runtime-unit analogue of
// newTestManagerWithManifest in crypto_manifest_lookup_test.go, built with no
// *Manager at all.
func newTestRuntimeWithManifest(t *testing.T, m *plugins.Manifest) *plugins.PluginRuntime {
	t.Helper()
	rt := plugins.NewPluginRuntime()
	rt.CommitLoaded(&plugins.DiscoveredPlugin{Manifest: m}, &runtimeStubHost{})
	return rt
}
