// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventvocab"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Round 3: event_emitter gaps.
//   - WithGameID option: 0% coverage.
//   - subjectNamespace: JS-native branch (events.<game>.<ns>.<...>) and
//     error shapes.
//   - bridgeActorKind: ActorPlayer + default branches.

// TestWithGameIDOptionStampsConfiguredGameIDIntoSubject verifies the
// option actually reroutes subject translation. Default is "main";
// with a custom provider we should see "events.<custom>.scene...".
func TestWithGameIDOptionStampsConfiguredGameIDIntoSubject(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		pluginActorResolver,
		plugins.WithGameID(func() string { return "alt-game" }),
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"x":1}`,
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.alt-game.scene.01TEST", msgs[0].Subject)
}

// TestWithGameIDEmptyProviderFallsBackToMain exercises the "game id provider
// returned empty" branch where the emitter falls back to "main".
func TestWithGameIDEmptyProviderFallsBackToMain(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		pluginActorResolver,
		plugins.WithGameID(func() string { return "" }),
	)
	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{}`,
	})
	require.NoError(t, err)
	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.main.scene.01TEST", msgs[0].Subject)
}

// TestEmitAcceptsJetStreamNativeSubject exercises the events.<game>.<ns>
// branch of subjectNamespace that rounds 1-2 did not reach.
func TestEmitAcceptsJetStreamNativeSubject(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		pluginActorResolver,
	)
	// Native subject already dot-delimited and events-prefixed.
	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "events.main.scene.01ABC",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"n":1}`,
	})
	require.NoError(t, err)
	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.main.scene.01ABC", msgs[0].Subject)
}

// TestEmitRejectsJetStreamSubjectMissingNamespaceTokens guards against the
// "events. ... too few tokens" error path.
func TestEmitRejectsJetStreamSubjectMissingNamespaceTokens(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{"only events prefix", "events.main"},
		{"empty namespace token", "events.main..suffix"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbustest.New(t)
			emitter := plugins.NewPluginEventEmitter(
				bus.Bus.Publisher(),
				func(string) *plugins.Manifest { return sceneManifest() },
				pluginActorResolver,
			)
			err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
				Subject: tc.subject,
				Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
				Payload: `{}`,
			})
			require.Error(t, err)
		})
	}
}

// TestEmitRejectsJetStreamSubjectWithInvalidNamespaceChars covers the
// namePattern.MatchString(ns) == false branch in subjectNamespace's
// JS-native arm.
func TestEmitRejectsJetStreamSubjectWithInvalidNamespaceChars(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		pluginActorResolver,
	)
	// Uppercase chars violate the ^[a-z](-?[a-z0-9])*$ plugin-name pattern.
	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "events.main.SCENE.01ABC",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{}`,
	})
	require.Error(t, err)
}

// systemActorResolver returns a core.Actor of kind System. Used to assert
// the manifest gate (Task 5 of the plugin actor-claim authentication
// rollout) refuses to let a plugin claim ActorSystem on emit. Manifest
// validation rejects "system" from actor_kinds_claimable, so no real
// manifest can ever pass; the resolver-driven path here is the lone
// regression guard for "what if a future code path forgets that".
func systemActorResolver(_ context.Context, _ string) (core.Actor, error) {
	return core.Actor{Kind: core.ActorSystem, ID: "system-actor-id"}, nil
}

// TestEmitRejectsSystemActorAtManifestGate asserts the gate refuses to
// vouch for ActorSystem because no manifest can declare "system" as
// claimable (validated at manifest load time per spec §3.2). Per spec
// §3.3.4 the binary-plugin path additionally re-anchors ActorSystem to
// ActorPlugin:<self> at token issuance (Task 8) before the gate sees it.
func TestEmitRejectsSystemActorAtManifestGate(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		systemActorResolver,
	)
	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{}`,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_ACTOR_KIND_NOT_CLAIMABLE")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}
