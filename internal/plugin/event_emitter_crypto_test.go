// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

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
	// These tests exercise the Phase 3a sensitivity fence directly, so
	// they MUST explicitly enable crypto. The default (off) bypasses
	// the fence — see TestEmitterDoesNotRunFenceWhenCryptoDisabled.
	return plugins.NewPluginEventEmitter(pub, lookup, resolve, plugins.WithCryptoEnabled(true))
}

// TestEmitterDoesNotRunFenceWhenCryptoDisabled is the regression test
// for the Phase 3a-merge-time bug where production manifests declare
// sensitivity: always for events the SDK cannot yet flag as sensitive
// (proto EmitEventRequest has no sensitive field). With crypto
// disabled (the Phase 3a default), the emitter MUST skip the fence
// and emit successfully even if the manifest would otherwise
// EVENT_SENSITIVITY_REQUIRED-reject.
func TestEmitterDoesNotRunFenceWhenCryptoDisabled(t *testing.T) {
	pub := &recordingPublisher{}
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
	// Construct without WithCryptoEnabled — defaults to false.
	emitter := plugins.NewPluginEventEmitter(pub, lookup, resolve)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene.01HXXXTESTSCENE000000000",
		Type:      pluginsdk.EventType("test-plugin:secret"),
		Payload:   `{}`,
		Sensitive: false, // would trigger INV-PLUGIN-30 if the fence ran
	}
	require.NoError(t, emitter.Emit(context.Background(), "test-plugin", intent),
		"crypto disabled: fence MUST be skipped")
	require.Len(t, pub.events, 1)
	assert.False(t, pub.events[0].Sensitive,
		"crypto disabled: event.Sensitive MUST be false regardless of manifest")
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
