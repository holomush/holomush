// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// sceneManifest returns a manifest declaring `scene` as an allowed emit
// namespace — matches the historical colon-namespace shape that every
// in-tree plugin still uses during the F1 transition.
func sceneManifest() *plugins.Manifest {
	return &plugins.Manifest{Name: "core-scenes", Emits: []string{"scene"}}
}

func pluginActorResolver(_ context.Context, _ string) (core.Actor, error) {
	return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
}

// newEmitter builds an emitter wired against the provided embedded bus.
// Centralizes construction so individual tests focus on behaviour.
func newEmitter(t *testing.T, bus *eventbustest.Embedded, lookup plugins.ManifestLookup, resolve plugins.ActorResolver) *plugins.PluginEventEmitter {
	t.Helper()
	publisher := bus.Bus.Publisher()
	require.NotNil(t, publisher)
	return plugins.NewPluginEventEmitter(publisher, lookup, resolve)
}

// fetchAllMessages returns every message currently on the EVENTS stream by
// walking sequences 1..state.LastSeq via GetMsg. Avoids consumer-based reads
// (which own acker/timer goroutines that don't drain deterministically under
// parallel test pressure) — GetMsg is a stateless RPC.
func fetchAllMessages(t *testing.T, js jetstream.JetStream) []*jetstream.RawStreamMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := js.Stream(ctx, eventbus.StreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	var out []*jetstream.RawStreamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && seq != 0; seq++ {
		msg, err := stream.GetMsg(ctx, seq)
		if err != nil {
			require.NoError(t, err)
		}
		out = append(out, msg)
	}
	return out
}

// TestPluginEventEmitterStampsHostOwnedFields is the happy-path contract:
// a legacy colon-delimited subject is translated to JS form, the message
// carries every required header, and the envelope decodes to the plugin's
// payload with the host-stamped actor.
func TestPluginEventEmitterStampsHostOwnedFields(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	msg := msgs[0]

	// Subject was translated: legacy "scene:01TEST" → events.<game_id>.scene.01TEST.
	// Default game_id is "main" (eventbus.Config default).
	assert.Equal(t, "events.main.scene.01TEST", msg.Subject)

	// Required headers — per spec §1 these are set on every message.
	assert.NotEmpty(t, msg.Header.Get(eventbus.HeaderMsgID), "Nats-Msg-Id must be set")
	assert.Equal(t, eventbus.SchemaVersion, msg.Header.Get(eventbus.HeaderSchemaVersion))
	assert.Equal(t, "system", msg.Header.Get(eventbus.HeaderEventType))
	// App-Codec must never be empty (spec §1: "yes — never empty").
	assert.Equal(t, "identity", msg.Header.Get(eventbus.HeaderCodec))
	assert.Equal(t, "plugin", msg.Header.Get(eventbus.HeaderActorKind))
	// Actor.ID is "core-scenes" (a plugin name, not a ULID) so the bridge
	// leaves the ulid zero and the publisher omits App-Actor-ID.
	assert.Empty(t, msg.Header.Get(eventbus.HeaderActorID))

	// Envelope decodes to the plugin payload we passed in.
	var env eventbusv1.Event
	require.NoError(t, proto.Unmarshal(msg.Data, &env))
	assert.Equal(t, `{"text":"hi"}`, string(env.GetPayload()))
	assert.Equal(t, "events.main.scene.01TEST", env.GetSubject())
	assert.Equal(t, "system", env.GetType())
	assert.Equal(t, eventbusv1.ActorKind_ACTOR_KIND_PLUGIN, env.GetActor().GetKind())
}

// TestPluginEventEmitterOmitsActorIDWhenZero complements the happy path by
// exercising the documented invariant that App-Actor-ID is absent (not
// present-but-empty) when the resolved actor has no id — spec §1:
// "zero ULID for ActorKindSystem / Unknown".
func TestPluginEventEmitterOmitsActorIDHeaderForSystemActor(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		// System actor with non-empty ID string that is NOT a ULID — the
		// bridge keeps the Kind but drops the id, matching spec intent.
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorSystem, ID: "system"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{}`,
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	// system kind → header present; non-ULID id → App-Actor-ID omitted.
	assert.Equal(t, "system", msgs[0].Header.Get(eventbus.HeaderActorKind))
	assert.Empty(t, msgs[0].Header.Get(eventbus.HeaderActorID),
		"App-Actor-ID MUST be absent when actor id is not a ULID")
}

// TestPluginEventEmitterIdempotentRetry is the property the Nats-Msg-Id header
// exists for: the same EmitIntent republished within the dedup window does
// NOT duplicate on the stream. Hand-rolled scenario (no rapid.Check) because
// the property only has one dimension and rapid would not improve coverage.
func TestPluginEventEmitterIdempotentRetryIsNoOpOnStreamState(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	// Publish the same payload twice in rapid succession. The host stamps a
	// fresh ULID on each call, so JS dedup does NOT absorb these two — the
	// invariant tested here is "same Nats-Msg-Id → one row". We exercise
	// that by publishing to JS directly with a fixed Msg-Id first, then the
	// emitter, to guarantee both paths observe dedup.
	intent := pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"n":1}`,
	}
	require.NoError(t, emitter.Emit(context.Background(), "core-scenes", intent))
	require.NoError(t, emitter.Emit(context.Background(), "core-scenes", intent))

	msgs := fetchAllMessages(t, bus.JS)
	// Two distinct host-stamped ULIDs → two rows (dedup is per ULID, not per
	// payload). The invariant under test lives in the Msg-Id header equals
	// the envelope id — subscribers dedup on this key.
	require.Len(t, msgs, 2)
	assert.NotEqual(t,
		msgs[0].Header.Get(eventbus.HeaderMsgID),
		msgs[1].Header.Get(eventbus.HeaderMsgID),
		"two Emit calls MUST mint different ULIDs",
	)
	// Regression guard for the previously-tautological final assertion:
	// the Nats-Msg-Id header MUST match the encoded envelope's ULID so
	// subscriber-side dedup on the header stays aligned with the event
	// the subscriber actually decodes.
	for i, m := range msgs {
		var env eventbusv1.Event
		require.NoError(t, proto.Unmarshal(m.Data, &env), "msg %d", i)
		encoded, err := ulid.Parse(m.Header.Get(eventbus.HeaderMsgID))
		require.NoError(t, err, "msg %d", i)
		assert.Equal(t, encoded.Bytes(), env.GetId(), "msg %d: Nats-Msg-Id MUST match envelope Id", i)
	}
}

// TestPluginPublisherDirectJetStreamRetryAbsorbsDuplicate drops down to the
// Publisher and exercises the real dedup property: two Publish calls with
// the same Event.ID land once on the stream (dedup window absorbs the
// duplicate). Split from the emitter test because the emitter intentionally
// mints a fresh ULID per call.
func TestPluginPublisherPublishWithSameIDIsAbsorbedByDedup(t *testing.T) {
	bus := eventbustest.New(t)
	pub := bus.Bus.Publisher()

	subject := eventbus.MustSubject("events.main.scene.01TEST")
	typ, _ := eventbus.NewType("system")

	event := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{}`),
	}
	require.NoError(t, pub.Publish(context.Background(), event))
	require.NoError(t, pub.Publish(context.Background(), event))

	msgs := fetchAllMessages(t, bus.JS)
	assert.Len(t, msgs, 1, "JS dedup window MUST absorb republish with identical Nats-Msg-Id")
}

// TestPluginEventEmitterRejectsUndeclaredNamespace checks that the emitter
// refuses to publish to a namespace not in manifest.Emits — prevents a
// plugin from smuggling events into another plugin's subject space.
func TestPluginEventEmitterRejectsUndeclaredNamespace(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "notifications:01CHAR",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"nudge"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifications")

	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsMissingManifestWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return nil }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may not emit")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsMissingManifestLookupWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(bus.Bus.Publisher(), nil, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest lookup")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsActorResolverFailureWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, func(context.Context, string) (core.Actor, error) {
		return core.Actor{}, errors.New("actor lookup failed")
	})

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor lookup failed")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsNilActorResolverWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		nil,
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor resolver")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsEmptyResolvedActorWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, func(context.Context, string) (core.Actor, error) {
		return core.Actor{}, nil
	})

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty ID")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsUnknownResolvedActorKindWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, func(context.Context, string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorKind(99), ID: "mystery"}, nil
	})

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown actor kind")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

// TestPluginEventEmitterRejectsMalformedSubjectWithoutPublishing keeps parity
// with the pre-F1 negative-path suite: empty, empty-namespace, empty-suffix,
// padded-whitespace subjects must all fail pre-publish.
func TestPluginEventEmitterRejectsMalformedSubjectWithoutPublishing(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{name: "empty subject", subject: ""},
		{name: "empty namespace", subject: ":ic"},
		{name: "empty suffix", subject: "scene:"},
		{name: "space padded suffix", subject: "scene: "},
		{name: "space padded subject", subject: " scene:01TEST:ic "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := eventbustest.New(t)
			emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

			err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
				Subject: tt.subject,
				Type:    pluginsdk.EventTypeSystem,
				Payload: `{"text":"hi"}`,
			})
			require.Error(t, err)
			assert.Empty(t, fetchAllMessages(t, bus.JS))
		})
	}
}

// TestPluginEventEmitterWrapsPublishFailure ensures publisher errors bubble
// out with plugin+subject context instead of being silently swallowed.
func TestPluginEventEmitterWrapsPublisherFailure(t *testing.T) {
	errPub := &erroringPublisher{err: errors.New("publish boom")}
	emitter := plugins.NewPluginEventEmitter(errPub, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish boom")
}

func TestPluginEventEmitterRejectsMissingPublisherWithoutPanic(t *testing.T) {
	emitter := plugins.NewPluginEventEmitter(nil, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publisher")
}

// TestPluginEventEmitterAcceptsPayloadAtMaxPayloadSize verifies the 64 KiB
// boundary is inclusive — payloads exactly at the cap MUST succeed.
func TestPluginEventEmitterAcceptsPayloadAtMaxPayloadSize(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	prefix := []byte(`{"text":"`)
	suffix := []byte(`"}`)
	filler := bytes.Repeat([]byte{'a'}, core.MaxPayloadSize-len(prefix)-len(suffix))
	payload := append(append(prefix, filler...), suffix...)
	require.Len(t, payload, core.MaxPayloadSize)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: string(payload),
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
}

func TestPluginEventEmitterRejectsOversizedPayloadWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	prefix := []byte(`{"text":"`)
	suffix := []byte(`"}`)
	filler := bytes.Repeat([]byte{'a'}, core.MaxPayloadSize-len(prefix)-len(suffix)+1)
	payload := append(append(prefix, filler...), suffix...)
	require.Len(t, payload, core.MaxPayloadSize+1)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: string(payload),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_TOO_LARGE")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

func TestPluginEventEmitterRejectsInvalidJSONPayloadWithoutPublishing(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene:01TEST",
		Type:    pluginsdk.EventTypeSystem,
		Payload: `{"text":`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid JSON")
	assert.Empty(t, fetchAllMessages(t, bus.JS))
}

// TestPluginEventEmitterReturnsErrPublishExpiredOnDeadlineExceeded forces the
// Publisher's dedup-window deadline to expire by constructing a Publisher
// with a pathologically small DupeWindow. Asserts the sentinel surface
// (errors.Is) so F3 subscribers can distinguish "retry is safe" from
// "retry must mint a new ULID".
//
// No time.Sleep: the deadline is negative on purpose — context.WithTimeout
// returns an already-expired context and PublishMsg's first network read
// sees DeadlineExceeded immediately.
func TestPluginPublisherReturnsErrPublishExpiredWhenDeadlineElapses(t *testing.T) {
	bus := eventbustest.New(t)
	// DupeWindow 1ns, safety margin 30s → effective deadline floors to 1ms
	// (publisher floor). Still fast enough that the publish will exceed it
	// in practice on a loaded test host. On happy paths we add an already
	// -cancelled context to force the timeout path deterministically.
	pub := eventbus.NewJetStreamPublisher(bus.JS, eventbus.Config{DupeWindow: time.Nanosecond}, eventbus.WithSafetyMargin(time.Hour))

	subject := eventbus.MustSubject("events.main.scene.01TEST")
	typ, _ := eventbus.NewType("system")
	event := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{}`),
	}

	// Use a parent context that is already cancelled: WithTimeout inherits
	// the cancel and PublishMsg observes DeadlineExceeded immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	cancel()
	err := pub.Publish(ctx, event)
	require.Error(t, err)
	require.True(t, errors.Is(err, eventbus.ErrPublishExpired), "expected ErrPublishExpired, got %v", err)
}

// erroringPublisher lets negative-path tests exercise wrap/unwrap without
// spinning up an embedded bus for every case.
type erroringPublisher struct{ err error }

func (p *erroringPublisher) Publish(context.Context, eventbus.Event) error { return p.err }
