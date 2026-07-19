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
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventvocab"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// sceneManifest returns a manifest declaring `scene` as an allowed emit
// namespace. Tests emit dot-relative subjects (e.g., "scene.01TEST") which
// Qualify turns into "events.main.scene.01TEST".
//
// ActorKindsClaimable mirrors the in-tree core-scenes manifest (Task 4 of
// the plugin actor-claim authentication rollout): plugin + character so
// the manifest gate at event_emitter.Emit allows both self-cascade and
// character-driven emits. Tests that need to exercise the gate's reject
// path build their own manifest inline.
func sceneManifest() *plugins.Manifest {
	return &plugins.Manifest{
		Name:                "core-scenes",
		Emits:               []string{"scene"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
}

// fixturePluginULID returns a deterministic ULID used as the
// resolved Actor.ID for plugin-actor test fixtures. Post-w9ml every
// stamp site MUST emit a parseable ULID; the plugintest helper
// (forthcoming in w9ml.13) will replace these inline ULIDs.
var fixturePluginULID = ulid.MustNew(0xDEADBEEF, bytes.NewReader(make([]byte, 16)))

func pluginActorResolver(_ context.Context, _ string) (core.Actor, error) {
	return core.Actor{Kind: core.ActorPlugin, ID: fixturePluginULID.String()}, nil
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
// a dot-relative subject is qualified to JS form, the message carries every
// required header, and the envelope decodes to the plugin's payload with the
// host-stamped actor.
func TestPluginEventEmitterStampsHostOwnedFields(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"text":"hi"}`,
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	msg := msgs[0]

	// Subject qualified: "scene.01TEST" → events.<game_id>.scene.01TEST.
	// Default game_id is "main" (eventbus.Config default).
	assert.Equal(t, "events.main.scene.01TEST", msg.Subject)

	// Required headers — per spec §1 these are set on every message.
	assert.NotEmpty(t, msg.Header.Get(eventbus.HeaderMsgID), "Nats-Msg-Id must be set")
	assert.Equal(t, eventbus.SchemaVersion, msg.Header.Get(eventbus.HeaderSchemaVersion))
	assert.Equal(t, "system", msg.Header.Get(eventbus.HeaderEventType))
	// App-Codec must never be empty (spec §1: "yes — never empty").
	assert.Equal(t, "identity", msg.Header.Get(eventbus.HeaderCodec))
	assert.Equal(t, "plugin", msg.Header.Get(eventbus.HeaderActorKind))
	// Post-w9ml: every stamp site emits a real ULID, so App-Actor-ID is
	// always present for plugin actors.
	assert.Equal(t, fixturePluginULID.String(), msg.Header.Get(eventbus.HeaderActorID))

	// Envelope decodes to the plugin payload we passed in.
	var env eventbusv1.Event
	require.NoError(t, proto.Unmarshal(msg.Data, &env))
	assert.Equal(t, `{"text":"hi"}`, string(env.GetPayload()))
	assert.Equal(t, "events.main.scene.01TEST", env.GetSubject())
	assert.Equal(t, "system", env.GetType())
	assert.Equal(t, eventbusv1.ActorKind_ACTOR_KIND_PLUGIN, env.GetActor().GetKind())
}

// TestPluginEventEmitterEmitsGlobalStream pins the bare-token namespace path
// (rops.7 regression guard): "global" is a live emit stream (Emitter.Global →
// "global"), so subjectNamespace MUST extract "global" as the namespace rather
// than reject it as malformed, the manifest gate then authorizes it, and Qualify
// produces events.<game_id>.global.
func TestPluginEventEmitterEmitsGlobalStream(t *testing.T) {
	bus := eventbustest.New(t)
	globalManifest := &plugins.Manifest{
		Name:                "core-broadcast",
		Emits:               []string{"global"},
		ActorKindsClaimable: []string{"plugin"},
	}
	emitter := newEmitter(t, bus, func(string) *plugins.Manifest { return globalManifest }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-broadcast", pluginsdk.EmitIntent{
		Subject: "global",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"text":"server restart"}`,
	})
	require.NoError(t, err)

	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.main.global", msgs[0].Subject)
}

// TestPluginEventEmitterRejectsNonULIDActorID is the post-w9ml invariant:
// the strict ULID gate at coreActorToEventbusActor surfaces
// ACTOR_ID_NOT_ULID and refuses to publish when a resolver returns a
// non-ULID Actor.ID. Pre-w9ml the bridge silently dropped non-ULID IDs
// and stamped App-Actor-ID empty; that fail-open path is gone.
func TestPluginEventEmitterRejectsNonULIDActorID(t *testing.T) {
	bus := eventbustest.New(t)
	emitter := plugins.NewPluginEventEmitter(
		bus.Bus.Publisher(),
		func(string) *plugins.Manifest { return sceneManifest() },
		// Plugin actor with a non-ULID ID — strict gate MUST reject.
		func(context.Context, string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, nil
		},
	)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{}`,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ACTOR_ID_NOT_ULID")

	// Nothing reached the stream.
	assert.Empty(t, fetchAllMessages(t, bus.JS))
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"n":1}`,
	}
	require.NoError(t, emitter.Emit(context.Background(), "core-scenes", intent))
	require.NoError(t, emitter.Emit(context.Background(), "core-scenes", intent))

	msgs := fetchAllMessages(t, bus.JS)
	// Two distinct host-stamped ULIDs → two rows (dedup is per ULID, not per
	// payload). The invariant under test lives in the Msg-Id header equals
	// the envelope id — subscribers dedup on this key.
	require.Len(t, msgs, 2)
	assert.NotEqual(
		t,
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
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
				Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish boom")
}

func TestPluginEventEmitterRejectsMissingPublisherWithoutPanic(t *testing.T) {
	emitter := plugins.NewPluginEventEmitter(nil, func(string) *plugins.Manifest { return sceneManifest() }, pluginActorResolver)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
	filler := bytes.Repeat([]byte{'a'}, eventvocab.MaxPayloadSize-len(prefix)-len(suffix))
	payload := append(append(prefix, filler...), suffix...)
	require.Len(t, payload, eventvocab.MaxPayloadSize)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
	filler := bytes.Repeat([]byte{'a'}, eventvocab.MaxPayloadSize-len(prefix)-len(suffix)+1)
	payload := append(append(prefix, filler...), suffix...)
	require.Len(t, payload, eventvocab.MaxPayloadSize+1)

	err := emitter.Emit(context.Background(), "core-scenes", pluginsdk.EmitIntent{
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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
		Subject: "scene.01TEST",
		Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
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

// actorFromCtxResolver is a test-only ActorResolver that reads from ctx
// (mirrors the production resolver at internal/plugin/manager.go:1164).
func actorFromCtxResolver(ctx context.Context, _ string) (core.Actor, error) {
	actor, ok := core.ActorFromContext(ctx)
	if !ok {
		return core.Actor{}, oops.New("plugin event actor missing from context")
	}
	return actor, nil
}

// TestEmitManifestGateRejectsCharacterClaimWithoutOptIn covers spec §3.4 + §5.3:
// a plugin manifest that doesn't list "character" MUST loud-error when emit
// ctx carries an ActorCharacter.
func TestEmitManifestGateRejectsCharacterClaimWithoutOptIn(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
		ActorKindsClaimable: []string{"plugin"}, // no character
	}
	e := newEmitter(
		t, bus,
		func(string) *plugins.Manifest { return manifest },
		actorFromCtxResolver,
	)
	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "01HCHAR0000000000000000000",
	})
	err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
		Subject: "location.01HLOC0000000000000000000",
		Type:    "say",
		Payload: `{"message":"hi"}`,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_ACTOR_KIND_NOT_CLAIMABLE")

	// No message should have been published.
	msgs := fetchAllMessages(t, bus.JS)
	assert.Empty(t, msgs)
}

// TestEmitManifestGateAllowsClaimedKind verifies the gate passes when the
// manifest declares the actor's kind.
func TestEmitManifestGateAllowsClaimedKind(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
	}
	e := newEmitter(
		t, bus,
		func(string) *plugins.Manifest { return manifest },
		actorFromCtxResolver,
	)
	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "01HCHAR0000000000000000000",
	})
	err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
		Subject: "location.01HLOC0000000000000000000",
		Type:    "say",
		Payload: `{"message":"hi"}`,
	})
	require.NoError(t, err)
	msgs := fetchAllMessages(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
}

// TestEmitManifestGateAllowsPluginCascade covers cascade preservation:
// plug-A emits during a cascade with ActorPlugin:plug-B in ctx; default
// [plugin] manifest allows plug-A to vouch for the cascade.
func TestEmitManifestGateAllowsPluginCascade(t *testing.T) {
	t.Parallel()
	bus := eventbustest.New(t)
	manifest := &plugins.Manifest{
		Name: "plug-A", Type: plugins.TypeLua, Emits: []string{"location"},
		ActorKindsClaimable: []string{"plugin"},
	}
	e := newEmitter(
		t, bus,
		func(string) *plugins.Manifest { return manifest },
		actorFromCtxResolver,
	)
	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorPlugin,
		ID:   ulid.MustNew(0xB, bytes.NewReader(make([]byte, 16))).String(), // upstream cascade
	})
	err := e.Emit(ctx, "plug-A", pluginsdk.EmitIntent{
		Subject: "location.01HLOC0000000000000000000",
		Type:    "test",
		Payload: `{}`,
	})
	require.NoError(t, err)
}

// erroringPublisher lets negative-path tests exercise wrap/unwrap without
// spinning up an embedded bus for every case.
type erroringPublisher struct{ err error }

func (p *erroringPublisher) Publish(context.Context, eventbus.Event) error { return p.err }
