// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubDEKManager is a deterministic eventbus.DEKManager fake. Every
// GetOrCreate returns the same codec.Key — the publisher tests only care
// about (a) that GetOrCreate is invoked when event.Sensitive=true and
// (b) that the returned key's ID/Version are stamped onto the published
// message via App-Dek-Ref / App-Dek-Version.
type stubDEKManager struct{ key codec.Key }

func (s stubDEKManager) GetOrCreate(_ context.Context, _ dek.ContextID, _ []dek.Participant) (codec.Key, error) {
	return s.key, nil
}

// newStubDEKManagerWithKey builds a stubDEKManager returning k from
// every GetOrCreate. The *testing.T parameter is accepted for future
// expansion (e.g., per-test cleanup hooks) and to match the pattern of
// other test factories in this file.
func newStubDEKManagerWithKey(_ *testing.T, k codec.Key) eventbus.DEKManager {
	return stubDEKManager{key: k}
}

// testKey32Bytes returns a deterministic 32-byte key suitable for the
// xchacha20poly1305-v1 codec. Deterministic so test failures are
// reproducible from the test name alone.
func testKey32Bytes(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// stubKeySelector is a codec.KeySelector whose Encrypt/Decrypt behaviour is
// controlled per-test. Used to exercise each Publisher branch that depends on
// the selector without touching production identity-codec policy.
type stubKeySelector struct {
	encryptName  codec.Name
	encryptLabel codec.KeyLabel
	encryptErr   error

	decryptKey codec.Key
	decryptErr error
}

func (s stubKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return s.encryptName, s.encryptLabel, s.encryptErr
}

func (s stubKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return s.decryptKey, s.decryptErr
}

// stubKeyProvider implements codec.KeyProvider returning a fixed error or key.
type stubKeyProvider struct {
	key codec.Key
	err error
}

func (s stubKeyProvider) Active(_ context.Context, _ codec.KeyLabel) (codec.Key, error) {
	return s.key, s.err
}

func (s stubKeyProvider) ByID(_ context.Context, _ codec.KeyID) (codec.Key, error) {
	return s.key, s.err
}

// errCodec returns a fixed error on Encode. Used to cover the
// EVENTBUS_CODEC_ENCODE_FAILED branch.
type errCodec struct{ name codec.Name }

func (e errCodec) Name() codec.Name { return e.name }
func (errCodec) Encode(_ context.Context, _ []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return nil, errors.New("encode boom")
}

func (errCodec) Decode(_ context.Context, _ []byte, _ codec.Key, _ []byte) ([]byte, error) {
	return nil, errors.New("decode boom")
}

func goodEvent(subject eventbus.Subject) eventbus.Event {
	return eventbus.Event{
		// Use the project-standard helper for Event IDs (core.NewULID is
		// monotonic within a millisecond); ulid.MustNew would mint a
		// non-monotonic fresh-entropy ULID which is the wrong tool for
		// event IDs per CLAUDE.md.
		ID:        core.NewULID(),
		Subject:   subject,
		Type:      eventbus.Type("scene.pose"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
	}
}

func TestPublisherRejectsNilJetStream(t *testing.T) {
	t.Parallel()
	p := eventbus.NewJetStreamPublisher(nil, eventbus.Config{})
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_PUBLISHER_NOT_READY")
}

func TestPublisherRejectsZeroEventID(t *testing.T) {
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher()
	ev := goodEvent("events.main.test")
	ev.ID = ulid.ULID{}
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_EVENT_ID_REQUIRED")
}

func TestPublisherRejectsInvalidSubject(t *testing.T) {
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher()
	ev := goodEvent("events.main.test")
	ev.Subject = "" // fails NewSubject (empty subject).
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	require.ErrorIs(t, err, eventbus.ErrInvalidSubject)
}

func TestPublisherRejectsInvalidType(t *testing.T) {
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher()
	ev := goodEvent("events.main.test")
	ev.Type = "NOT-a-valid-type" // fails NewType (uppercase / dash).
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	require.ErrorIs(t, err, eventbus.ErrInvalidType)
}

func TestPublisherRejectsOversizedPayload(t *testing.T) {
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher()
	ev := goodEvent("events.main.test")
	ev.Payload = make([]byte, eventbus.MaxPayloadSize+1)
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_PAYLOAD_TOO_LARGE")
	require.ErrorIs(t, err, eventbus.ErrPayloadTooLarge)
}

func TestPublisherReturnsCodecSelectError(t *testing.T) {
	embedded := eventbustest.New(t)
	sentinel := errors.New("selector boom")
	p := embedded.Bus.Publisher(eventbus.WithCodecSelector(stubKeySelector{
		encryptErr: sentinel,
	}))
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_SELECT_FAILED")
	require.ErrorIs(t, err, sentinel)
}

func TestPublisherReturnsUnknownCodecError(t *testing.T) {
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher(eventbus.WithCodecSelector(stubKeySelector{
		encryptName: codec.Name("totally-unknown-codec"),
	}))
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_UNKNOWN")
}

func TestPublisherRequiresKeyProviderForNonIdentityCodec(t *testing.T) {
	// Register a non-identity codec for the test so we can reach the
	// "non-identity codec requires a KeyProvider" branch without changing
	// prod codec enumeration.
	stubName := codec.Name("stub-encrypt-v1")
	restore := codec.RegisterForTest(errCodec{name: stubName})
	t.Cleanup(restore)

	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher(eventbus.WithCodecSelector(stubKeySelector{
		encryptName:  stubName,
		encryptLabel: codec.KeyLabel("k1"),
	}))
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_KEY_PROVIDER_MISSING")
}

func TestPublisherPropagatesKeyFetchError(t *testing.T) {
	stubName := codec.Name("stub-encrypt-v2")
	restore := codec.RegisterForTest(errCodec{name: stubName})
	t.Cleanup(restore)

	embedded := eventbustest.New(t)
	fetchErr := errors.New("KMS down")
	p := embedded.Bus.Publisher(
		eventbus.WithCodecSelector(stubKeySelector{
			encryptName:  stubName,
			encryptLabel: codec.KeyLabel("k1"),
		}),
		eventbus.WithKeyProvider(stubKeyProvider{err: fetchErr}),
	)
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_KEY_FETCH_FAILED")
	require.ErrorIs(t, err, fetchErr)
}

func TestPublisherPropagatesCodecEncodeError(t *testing.T) {
	stubName := codec.Name("stub-encode-fail")
	restore := codec.RegisterForTest(errCodec{name: stubName})
	t.Cleanup(restore)

	embedded := eventbustest.New(t)
	// Identity-key path avoids the KeyProvider requirement: the selector
	// routes encryption through the stub codec but the codec itself uses
	// the identity-like Key{}.
	p := embedded.Bus.Publisher(
		eventbus.WithCodecSelector(stubKeySelector{
			encryptName: stubName,
		}),
		eventbus.WithKeyProvider(stubKeyProvider{}),
	)
	err := p.Publish(context.Background(), goodEvent("events.main.test"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_ENCODE_FAILED")
}

func TestPublisherParentContextCancelledSurfacesPublishFailed(t *testing.T) {
	// A parent ctx cancelled before Publish → js.PublishMsg returns an error
	// that wraps ctx.Err() (context.Canceled). Our Publisher path only
	// remaps context.DeadlineExceeded to ErrPublishExpired; Canceled returns
	// EVENTBUS_PUBLISH_FAILED. Guards the non-deadline branch of the error
	// mapping.
	embedded := eventbustest.New(t)
	p := embedded.Bus.Publisher()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Publish(ctx, goodEvent("events.main.cancelled"))
	require.Error(t, err)
	// Both oopsErr and the wrapped original are acceptable — we just want
	// to confirm we landed in the generic publish-failed branch (or the
	// deadline branch on a pathologically slow box that also cancels).
	if !errors.Is(err, eventbus.ErrPublishExpired) {
		errutil.AssertErrorCode(t, err, "EVENTBUS_PUBLISH_FAILED")
	}
}

func TestPublisherStampsAllRequiredHeaders(t *testing.T) {
	// Subscribe and assert every required header is present. This guards
	// the invariant that missing/empty headers are a publisher bug.
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	subject := eventbus.Subject("events.main.pub.hdr")
	sessID := freshSessionID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessID, testIdentity(), []eventbus.Subject{subject}, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev := goodEvent(subject)
	actorULID := core.NewULID()
	ev.Actor = eventbus.Actor{
		Kind: eventbus.ActorKindPlugin,
		ID:   actorULID,
	}
	require.NoError(t, pub.Publish(ctx, ev))

	// Subscriber decoded headers; we verify via the Event returned. A
	// regression in any header-stamping path (actor kind, actor ID,
	// subject, timestamp) must fail this test, not just ID/Type.
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	got := d.Event()
	require.Equal(t, ev.ID, got.ID)
	require.Equal(t, ev.Type, got.Type)
	require.Equal(t, ev.Subject, got.Subject, "Subject header must round-trip")
	require.Equal(t, ev.Actor.Kind, got.Actor.Kind, "Actor-Kind header must round-trip")
	require.Equal(t, ev.Actor.ID, got.Actor.ID, "Actor-ID must round-trip")
	// Timestamp fidelity is to millisecond precision on the JS path.
	require.WithinDuration(t, ev.Timestamp, got.Timestamp, 1*time.Millisecond, "Timestamp header must round-trip")
	require.NoError(t, d.Ack())
}

// TestPublisherPreservesNanoseconds gates INV-STORE-4. The publisher MUST
// NOT truncate the event timestamp before AAD construction or envelope
// marshal. After the BIGINT-ns migration, AAD reconstruction at read
// time receives the full-precision timestamp from PG, so byte-equal AAD
// is structurally guaranteed.
//
// This test inverts the prior TestPublisherTruncatesTimestampToMicrosecond:
// it asserts that sub-µs nanos SURVIVE the publish path.
func TestPublisherPreservesNanoseconds(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()

	subject := eventbus.Subject("events.test.publisher.preserves.ns")
	sessID := freshSessionID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessID, testIdentity(), []eventbus.Subject{subject}, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev := goodEvent(subject)
	// Sub-µs nanosecond component on purpose.
	ev.Timestamp = time.Date(2026, 5, 14, 12, 34, 56, 123456789, time.UTC)
	require.Equal(t, 789, ev.Timestamp.Nanosecond()%1000,
		"test fixture sanity: pre-publish timestamp MUST carry sub-µs nanos")

	require.NoError(t, pub.Publish(ctx, ev))

	d, err := stream.Next(ctx)
	require.NoError(t, err)
	got := d.Event()
	require.NoError(t, d.Ack())

	assert.Equal(t, 789, got.Timestamp.Nanosecond()%1000,
		"INV-STORE-4: publisher MUST preserve sub-µs nanoseconds; sub-µs digits MUST survive")
	assert.Equal(t, ev.Timestamp, got.Timestamp.UTC(),
		"INV-STORE-4: published timestamp MUST equal source timestamp at full precision")
}

func TestActorKindStringCoversAllVariants(t *testing.T) {
	t.Parallel()
	cases := map[eventbus.ActorKind]string{
		eventbus.ActorKindCharacter: "character",
		eventbus.ActorKindPlayer:    "player",
		eventbus.ActorKindSystem:    "system",
		eventbus.ActorKindPlugin:    "plugin",
		eventbus.ActorKindUnknown:   "unknown",
	}
	for kind, want := range cases {
		require.Equal(t, want, kind.String())
	}
}

// TestPublisherCopiesRenderingIntoEnvelope is INV-GW-3a. JetStreamPublisher
// MUST copy event.Rendering into the proto envelope before Marshal so
// subscribers see the same Rendering on the read side.
func TestPublisherCopiesRenderingIntoEnvelope(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()

	rendering := &eventbus.RenderingMetadata{
		Category:            "communication",
		Format:              "speech",
		Label:               "says",
		DisplayTarget:       eventbus.EventChannelTerminal,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	subject := eventbus.Subject("events.main.character.01ABC")
	sessID := freshSessionID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessID, testIdentity(), []eventbus.Subject{subject}, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   subject,
		Type:      eventbus.Type("core-communication:say"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: core.NewULID()},
		Payload:   []byte(`{"message":"hello"}`),
		Rendering: rendering,
	}
	require.NoError(t, pub.Publish(ctx, ev))
	embedded.AwaitStreamLastSeq(t, 1, 0)

	d, err := stream.Next(ctx)
	require.NoError(t, err)
	got := d.Event()
	require.NotNil(t, got.Rendering, "subscriber-side decode must populate Rendering")
	assert.Equal(t, rendering.Category, got.Rendering.Category)
	assert.Equal(t, rendering.Format, got.Rendering.Format)
	assert.Equal(t, rendering.Label, got.Rendering.Label)
	assert.Equal(t, rendering.DisplayTarget, got.Rendering.DisplayTarget)
	assert.Equal(t, rendering.SourcePlugin, got.Rendering.SourcePlugin)
	assert.Equal(t, rendering.SourcePluginVersion, got.Rendering.SourcePluginVersion)
	require.NoError(t, d.Ack())
}

func TestPublisherMergesHeadersIntoNatsMsg(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()

	ev := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.character.01ABC"),
		Type:      eventbus.Type("core-communication:say"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{"message":"hi"}`),
		// App-Rendering is reserved (written by RenderingPublisher only).
		// Use a different App-* key to test the merge path.
		Headers: map[string]string{"App-Custom-Trace": "trace-value-1"},
	}
	require.NoError(t, pub.Publish(context.Background(), ev))
	embedded.AwaitStreamLastSeq(t, 1, 0)

	msgs := embedded.RawMessagesOnSubject(t, "events.main.character.01ABC", 10, 0)
	require.Len(t, msgs, 1)
	assert.Equal(t, "trace-value-1", msgs[0].Header.Get("App-Custom-Trace"))
	// System headers still present.
	assert.NotEmpty(t, msgs[0].Header.Get("Nats-Msg-Id"))
}

func TestPublisherCollidingHeaderPanicsInTests(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()

	ev := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.character.01ABC"),
		Type:      eventbus.Type("core-communication:say"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{"message":"hi"}`),
		Headers:   map[string]string{"Nats-Msg-Id": "naughty"},
	}
	assert.Panics(t, func() {
		_ = pub.Publish(context.Background(), ev)
	})
}

func TestIdentityKeySelectorReturnsIdentityAndNoKey(t *testing.T) {
	// The package-internal identityKeySelector is exercised end-to-end when
	// no WithCodecSelector is provided. This test covers the default path
	// indirectly by publishing without a selector and subscribing back.
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()

	subject := eventbus.Subject("events.main.default.sel")
	sessID := freshSessionID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessID, testIdentity(), []eventbus.Subject{subject}, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	want := goodEvent(subject)
	require.NoError(t, pub.Publish(ctx, want))

	d, err := stream.Next(ctx)
	require.NoError(t, err)
	// Prove the default identityKeySelector path: the round-tripped
	// event's identity (ID + Subject + Type) must match exactly and the
	// delivery MUST ack cleanly. A broken selector would either drop the
	// event, mis-key it across subjects, or produce a different ID.
	got := d.Event()
	require.Equal(t, want.ID, got.ID, "identityKeySelector must preserve the event ID")
	require.Equal(t, subject, got.Subject, "identityKeySelector must preserve the subject")
	require.Equal(t, want.Type, got.Type, "identityKeySelector must preserve the type")
	require.NoError(t, d.Ack())
}

// TestHeaderConstantsIncludeDekRefAndDekVersion pins the on-the-wire spelling
// of the App-Dek-Ref and App-Dek-Version headers. The audit projection and
// any downstream decoder reads these by literal string; renaming the
// constants without updating subscribers would silently lose dek metadata.
func TestHeaderConstantsIncludeDekRefAndDekVersion(t *testing.T) {
	if eventbus.HeaderDekRef != "App-Dek-Ref" {
		t.Fatalf("HeaderDekRef = %q, want %q", eventbus.HeaderDekRef, "App-Dek-Ref")
	}
	if eventbus.HeaderDekVersion != "App-Dek-Version" {
		t.Fatalf("HeaderDekVersion = %q, want %q", eventbus.HeaderDekVersion, "App-Dek-Version")
	}
}

// TestPublisherWithoutDEKManagerRejectsSensitiveEvent guards the fence:
// a sensitive event MUST NOT publish via the legacy identity/selector path
// when no DEK manager is wired. The publisher returns
// EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER instead of silently treating
// the payload as plaintext.
func TestPublisherWithoutDEKManagerRejectsSensitiveEvent(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher() // no WithDEKManager

	ev := goodEvent("events.main.scene.01HXXXSCENEID000000000")
	ev.Sensitive = true

	err := pub.Publish(context.Background(), ev)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER")
}

// TestPublisherWithDEKManagerStampsCryptoHeadersOnSensitiveEvent asserts
// that when a DEK manager is wired and event.Sensitive=true, the publisher
// (a) routes encryption through xchacha20poly1305-v1, and (b) stamps both
// App-Dek-Ref and App-Dek-Version headers from the codec.Key returned by
// the DEK manager. These headers feed events_audit's dek_ref/dek_version
// columns; missing or wrong values silently strand audit rows.
func TestPublisherWithDEKManagerStampsCryptoHeadersOnSensitiveEvent(t *testing.T) {
	mgr := newStubDEKManagerWithKey(t, codec.Key{
		ID:      42,
		Version: 3,
		Bytes:   testKey32Bytes(t),
	})

	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher(eventbus.WithDEKManager(mgr))

	subject := eventbus.Subject("events.main.scene.01HXXXSCENEID000000000")
	ev := goodEvent(subject)
	ev.Sensitive = true

	require.NoError(t, pub.Publish(context.Background(), ev))
	embedded.AwaitStreamLastSeq(t, 1, 0)

	msgs := embedded.RawMessagesOnSubject(t, string(subject), 1, 0)
	require.Len(t, msgs, 1)
	assert.Equal(t, "xchacha20poly1305-v1", msgs[0].Header.Get(eventbus.HeaderCodec))
	assert.Equal(t, "42", msgs[0].Header.Get(eventbus.HeaderDekRef))
	assert.Equal(t, "3", msgs[0].Header.Get(eventbus.HeaderDekVersion))
}
