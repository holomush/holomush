// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
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
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
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
	assert.False(t, events[0].Timestamp.IsZero())
	assert.Equal(t, []byte(`{"text":"hi"}`), events[0].Payload)
}

func TestPluginEventEmitterRejectsUndeclaredNamespace(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "notifications:01CHAR",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"nudge"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifications")

	events, replayErr := store.Replay(context.Background(), "notifications:01CHAR", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsMissingManifestWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return nil },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may not emit")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsMissingManifestLookupWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		nil,
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest lookup")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsActorResolverFailureWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{}, errors.New("actor lookup failed")
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor lookup failed")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsNilActorResolverWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		nil,
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor resolver")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsEmptyResolvedActorWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) { return core.Actor{}, nil },
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty ID")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsUnknownResolvedActorKindWithoutAppending(t *testing.T) {
	store := core.NewMemoryEventStore()
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorKind(99), ID: "mystery"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown actor kind")

	events, replayErr := store.Replay(context.Background(), "scene:01TEST:ic", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Empty(t, events)
}

func TestPluginEventEmitterRejectsMalformedStreamWithoutAppending(t *testing.T) {
	tests := []struct {
		name   string
		stream string
	}{
		{name: "empty stream", stream: ""},
		{name: "empty namespace", stream: ":ic"},
		{name: "empty suffix", stream: "scene:"},
		{name: "space padded suffix", stream: "scene: "},
		{name: "space padded stream", stream: " scene:01TEST:ic "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := core.NewMemoryEventStore()
			emitter := NewPluginEventEmitter(
				store,
				func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
				func(context.Context, string) (core.Actor, error) {
					return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
				},
			)

			err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
				Stream:  tt.stream,
				Type:    pluginsdk.EventTypeSay,
				Payload: `{"text":"hi"}`,
			})
			require.Error(t, err)

			events, replayErr := store.Replay(context.Background(), tt.stream, ulid.ULID{}, 10)
			require.NoError(t, replayErr)
			assert.Empty(t, events)
		})
	}
}

func TestPluginEventEmitterWrapsStoreAppendFailure(t *testing.T) {
	store := &failingEventStore{err: errors.New("append failed")}
	emitter := NewPluginEventEmitter(
		store,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append failed")
}

func TestPluginEventEmitterRejectsMissingEventStoreWithoutAppending(t *testing.T) {
	emitter := NewPluginEventEmitter(
		nil,
		func(string) *Manifest { return &Manifest{Name: "core-scenes", Emits: []string{"scene"}} },
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Stream:  "scene:01TEST:ic",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event store")
}

type failingEventStore struct {
	err error
}

func (s *failingEventStore) Append(context.Context, core.Event) error {
	return s.err
}

func (s *failingEventStore) Replay(context.Context, string, ulid.ULID, int) ([]core.Event, error) {
	return nil, nil
}

func (s *failingEventStore) LastEventID(context.Context, string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}

func (s *failingEventStore) Subscribe(context.Context, string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}
