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
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func newSeededTestRegistry(t *testing.T) *core.VerbRegistry {
	t.Helper()
	r := core.NewVerbRegistry()
	require.NoError(t, r.RegisterWithSource(core.VerbRegistration{
		Type:          "core-communication:say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "0.1.0"))
	return r
}

// TestRenderingPublisherStampsEventRendering is INV-EVENTBUS-2.
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

// TestRenderingPublisherStampsAppRenderingHeader is INV-EVENTBUS-15. The
// header value MUST encode the same RenderingMetadata as event.Rendering,
// using protojson.MarshalOptions{UseProtoNames, UseEnumNumbers=false}.
func TestRenderingPublisherStampsAppRenderingHeader(t *testing.T) {
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
	require.NotNil(t, got.Headers)
	headerJSON, ok := got.Headers["App-Rendering"]
	require.True(t, ok, "App-Rendering header missing")

	// Compare the raw header JSON against the canonical marshal of the
	// envelope's Rendering. Any drift in marshal options (e.g., enum numbers,
	// proto-vs-camel field names) shows up as a byte-for-byte mismatch
	// instead of being smoothed over by an Unmarshal/Marshal round-trip.
	envelopeMD := eventbus.RenderingToProto(got.Rendering)
	opts := protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: false, EmitUnpopulated: true}
	envelopeCanonical, err := opts.Marshal(envelopeMD)
	require.NoError(t, err)
	assert.Equal(t, string(envelopeCanonical), headerJSON)

	// Decode header for the sanity-check assertions below.
	headerMD := &corev1.RenderingMetadata{}
	require.NoError(t, protojson.Unmarshal([]byte(headerJSON), headerMD))
	assert.Equal(t, "communication", headerMD.GetCategory())
	assert.Equal(t, "speech", headerMD.GetFormat())
}

// TestRenderingPublisherUnknownVerb is INV-EVENTBUS-3. Registry-miss returns
// EMIT_UNKNOWN_VERB and does NOT publish.
func TestRenderingPublisherUnknownVerb(t *testing.T) {
	inner := &fakePublisher{}
	rp := eventbus.NewRenderingPublisher(inner, core.NewVerbRegistry()) // empty registry

	ev := eventbus.Event{
		ID:        ulid.Make(),
		Subject:   eventbus.Subject("events.main.character.01ABC"),
		Type:      eventbus.Type("core-communication:say"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter},
		Payload:   []byte(`{}`),
	}
	err := rp.Publish(context.Background(), ev)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_UNKNOWN_VERB")
	assert.Empty(t, inner.published, "must not publish on unknown verb")
}

// TestRenderingPublisherSourcePluginVersionForBuiltin is INV-EVENTBUS-10 for builtins.
// host-owned event types (registered via BootstrapVerbRegistry) MUST have
// source_plugin == "builtin" and source_plugin_version == "host-<binary version>".
func TestRenderingPublisherSourcePluginVersionForBuiltin(t *testing.T) {
	r, err := core.BootstrapVerbRegistry("0.4.2-test")
	require.NoError(t, err)

	inner := &fakePublisher{}
	rp := eventbus.NewRenderingPublisher(inner, r)

	ev := eventbus.Event{
		ID:      ulid.Make(),
		Subject: eventbus.Subject("events.main.character.01ABC"),
		Type:    eventbus.Type("arrive"), // builtin
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload: []byte(`{}`),
	}
	require.NoError(t, rp.Publish(context.Background(), ev))

	require.Len(t, inner.published, 1)
	got := inner.published[0].Rendering
	require.NotNil(t, got)
	assert.Equal(t, "builtin", got.SourcePlugin)
	assert.Equal(t, "host-0.4.2-test", got.SourcePluginVersion)
}

// TestRenderingPublisherSourcePluginVersionForPlugin is INV-EVENTBUS-10 for plugins.
// Plugin-owned event types MUST have source_plugin = manifest name and
// source_plugin_version = manifest version.
func TestRenderingPublisherSourcePluginVersionForPlugin(t *testing.T) {
	r, err := core.BootstrapVerbRegistry("0.4.2-test")
	require.NoError(t, err)
	require.NoError(t, r.RegisterWithSource(core.VerbRegistration{
		Type:          "core-communication:say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "0.1.0"))

	inner := &fakePublisher{}
	rp := eventbus.NewRenderingPublisher(inner, r)

	ev := eventbus.Event{
		ID:      ulid.Make(),
		Subject: eventbus.Subject("events.main.character.01ABC"),
		Type:    eventbus.Type("core-communication:say"),
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindCharacter},
		Payload: []byte(`{}`),
	}
	require.NoError(t, rp.Publish(context.Background(), ev))

	require.Len(t, inner.published, 1)
	got := inner.published[0].Rendering
	require.NotNil(t, got)
	assert.Equal(t, "core-communication", got.SourcePlugin)
	assert.Equal(t, "0.1.0", got.SourcePluginVersion)
}

// fakePublisher captures events for inspection.
type fakePublisher struct {
	published []eventbus.Event
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, ev eventbus.Event) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, ev)
	return nil
}
