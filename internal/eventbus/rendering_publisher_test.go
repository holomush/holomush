// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

func newSeededTestRegistry(t *testing.T) *core.VerbRegistry {
	t.Helper()
	r := core.NewVerbRegistry()
	require.NoError(t, r.RegisterWithSource(core.VerbRegistration{
		Type:          "core-communication:say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "0.1.0"))
	return r
}

// TestRenderingPublisherStampsEventRendering is INV-GW-2.
// RenderingPublisher.Publish MUST stamp event.Rendering from the verb
// registry before publishing.
func TestRenderingPublisherStampsEventRendering(t *testing.T) {
	inner := &fakePublisher{}
	rp := eventbus.NewRenderingPublisher(inner, newSeededTestRegistry(t))

	ev := eventbus.Event{
		ID:        ulid.Make(),
		Subject:   eventbus.Subject("events.main.character.01ABC"),
		Type:      eventbus.Type("core-communication:say"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter},
		Payload:   []byte(`{"message":"hi"}`),
	}
	require.NoError(t, rp.Publish(context.Background(), ev))

	require.Len(t, inner.published, 1)
	got := inner.published[0]
	require.NotNil(t, got.Rendering)
	assert.Equal(t, "communication", got.Rendering.Category)
	assert.Equal(t, "speech", got.Rendering.Format)
	assert.Equal(t, "says", got.Rendering.Label)
	assert.Equal(t, eventbus.EventChannelTerminal, got.Rendering.DisplayTarget)
	assert.Equal(t, "core-communication", got.Rendering.SourcePlugin)
	assert.Equal(t, "0.1.0", got.Rendering.SourcePluginVersion)
}

// fakePublisher captures events for inspection.
type fakePublisher struct {
	published []eventbus.Event
	err       error
}

func (f *fakePublisher) Publish(ctx context.Context, ev eventbus.Event) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, ev)
	return nil
}
