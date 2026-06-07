// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// recordingPublisher captures the eventbus.Event passed to Publish so
// tests can assert on its host-internal fields (Sensitive, etc.).
type recordingPublisher struct{ events []eventbus.Event }

func (r *recordingPublisher) Publish(_ context.Context, e eventbus.Event) error {
	r.events = append(r.events, e)
	return nil
}

func newCryptoTestEmitter(t *testing.T, pub eventbus.Publisher, manifest *plugins.Manifest) *plugins.PluginEventEmitter {
	t.Helper()
	lookup := func(name string) *plugins.Manifest {
		if name == "test-plugin" {
			return manifest
		}
		return nil
	}
	// ActorResolver returns core.Actor — Actor.ID is a string per
	// internal/core/event.go:170. ActorPlugin lives in package core
	// (internal/core/event.go:148).
	resolve := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: fixturePluginULID.String()}, nil
	}
	// The host-side sensitivity fence runs on every emit (no gate), so a
	// plainly-constructed emitter exercises it directly.
	return plugins.NewPluginEventEmitter(pub, lookup, resolve)
}

// TestEmitterRunsFenceUnconditionallyWithoutFlag is the holomush-dj95.3
// regression. With the WithCryptoEnabled gate removed, the host-side
// sensitivity fence runs on EVERY emit: the plugin manifest declaration is
// the single source of truth with no runtime override. A plainly-constructed
// emitter (no options — mirroring production after the fossil deletion) MUST
// stamp Sensitive per the manifest and MUST reject an under-claimed
// sensitivity:always emit, where the old gate would have silently bypassed it
// and shipped plaintext.
func TestEmitterRunsFenceUnconditionallyWithoutFlag(t *testing.T) {
	manifest := newSensitiveTestManifest([]plugins.CryptoEmit{
		{EventType: "test-plugin:secret", Sensitivity: plugins.SensitivityAlways},
	})
	lookup := func(name string) *plugins.Manifest {
		if name == "test-plugin" {
			return manifest
		}
		return nil
	}
	resolve := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: fixturePluginULID.String()}, nil
	}

	t.Run("manifest always + claim true stamps Sensitive=true with no flag", func(t *testing.T) {
		pub := &recordingPublisher{}
		emitter := plugins.NewPluginEventEmitter(pub, lookup, resolve)
		intent := pluginsdk.EmitIntent{
			Subject:   "scene.01HXXXTESTSCENE000000000",
			Type:      pluginsdk.EventType("test-plugin:secret"),
			Payload:   `{}`,
			Sensitive: true,
		}
		require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent))
		require.Len(t, pub.events, 1)
		assert.True(t, pub.events[0].Sensitive,
			"manifest sensitivity:always MUST stamp Sensitive=true with no game-config flag set")
	})

	t.Run("manifest always + claim false is rejected unconditionally", func(t *testing.T) {
		pub := &recordingPublisher{}
		emitter := plugins.NewPluginEventEmitter(pub, lookup, resolve)
		intent := pluginsdk.EmitIntent{
			Subject:   "scene.01HXXXTESTSCENE000000000",
			Type:      pluginsdk.EventType("test-plugin:secret"),
			Payload:   `{}`,
			Sensitive: false, // under-claims an always-sensitive event
		}
		err := emitter.Emit(context.Background(), "test-plugin", intent)
		require.Error(t, err, "the fence MUST reject an under-claimed always-sensitive emit (no bypass)")
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok, "error must be an oops error")
		assert.Equal(t, "EVENT_SENSITIVITY_REQUIRED", oopsErr.Code())
		assert.Empty(t, pub.events, "a rejected emit MUST NOT publish")
	})
}

func TestEmitterStampsSensitiveTrueForManifestMayPlusClaimTrue(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest([]plugins.CryptoEmit{
		{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene.01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   `{"text":"hi"}`,
		Sensitive: true,
	}
	require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent))

	require.Len(t, pub.events, 1)
	assert.True(t, pub.events[0].Sensitive, "manifest=may + claim=true must set event.Sensitive")
}

func TestEmitterStampsSensitiveFalseForManifestMayPlusClaimFalse(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest([]plugins.CryptoEmit{
		{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene.01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   `{"text":"hi"}`,
		Sensitive: false,
	}
	require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent))

	require.Len(t, pub.events, 1)
	assert.False(t, pub.events[0].Sensitive)
}

func TestEmitterRejectsClaimTrueOnManifestNeverEvent(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest([]plugins.CryptoEmit{
		{EventType: "test-plugin:pose", Sensitivity: plugins.SensitivityNever},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene.01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:pose"),
		Payload:   `{}`,
		Sensitive: true, // INV-PLUGIN-29 over-claim
	}
	err := emitter.Emit(context.Background(), "test-plugin", intent)
	require.Error(t, err)
	assert.Empty(t, pub.events, "rejected emit must not publish")
}

func TestEmitterRejectsClaimFalseOnManifestAlwaysEvent(t *testing.T) {
	pub := &recordingPublisher{}
	manifest := newSensitiveTestManifest([]plugins.CryptoEmit{
		{EventType: "test-plugin:secret", Sensitivity: plugins.SensitivityAlways},
	})
	emitter := newCryptoTestEmitter(t, pub, manifest)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene.01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:secret"),
		Payload:   `{}`,
		Sensitive: false, // INV-PLUGIN-30 under-claim
	}
	err := emitter.Emit(context.Background(), "test-plugin", intent)
	require.Error(t, err)
	assert.Empty(t, pub.events)
}

// newSensitiveTestManifest constructs a minimal valid Manifest with a
// crypto.emits block. ActorKindsClaimable is []string per manifest.go:84
// (validated/normalized to lowercase strings — "plugin", "character",
// etc.). Crypto is *CryptoSection per manifest.go:107. The plugin name
// is fixed to "test-plugin" to match newCryptoTestEmitter's lookup, and
// the emit namespace is fixed to "scene" so the subject "scene...." passes
// the manifest gate.
func newSensitiveTestManifest(emits []plugins.CryptoEmit) *plugins.Manifest {
	return &plugins.Manifest{
		Name:                "test-plugin",
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin"},
		Crypto:              &plugins.CryptoSection{Emits: emits},
	}
}
