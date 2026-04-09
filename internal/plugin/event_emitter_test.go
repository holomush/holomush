// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestPluginEventEmitterStampsHostOwnedFields(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) core.Actor { return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"} },
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.NoError(t, err)

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	assert.NotEqual(t, ulid.ULID{}, events[0].ID)
	assert.Equal(t, core.ActorPlugin, events[0].Actor.Kind)
	assert.Equal(t, "core-scenes", events[0].Actor.ID)
	assert.Equal(t, "scene:01TEST:ic", events[0].Stream)
	assert.Equal(t, core.EventType(pluginsdk.EventTypeSay), events[0].Type)
	assert.Equal(t, []byte(`{"text":"hi"}`), events[0].Payload)
}

func TestPluginEventEmitterRejectsUndeclaredNamespace(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) core.Actor { return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"} },
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "notifications:01CHAR",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"nudge"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifications")
}
