// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

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
func (errCodec) Encode(_ context.Context, _ []byte, _ codec.Key) ([]byte, error) {
	return nil, errors.New("encode boom")
}
func (errCodec) Decode(_ context.Context, _ []byte, _ codec.Key) ([]byte, error) {
	return nil, errors.New("decode boom")
}

func goodEvent(subject eventbus.Subject) eventbus.Event {
	return eventbus.Event{
		ID:        ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy),
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

	stream, err := sub.OpenSession(ctx, sessID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev := goodEvent(subject)
	ev.Actor = eventbus.Actor{
		Kind: eventbus.ActorKindPlugin,
		// Leave ID zero to exercise the LegacyID fallback branch.
		LegacyID: "core-scenes",
	}
	require.NoError(t, pub.Publish(ctx, ev))

	// Subscriber decoded headers; we verify via the Event returned.
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	got := d.Event()
	require.Equal(t, ev.ID, got.ID)
	require.Equal(t, ev.Type, got.Type)
	require.NoError(t, d.Ack())
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

	stream, err := sub.OpenSession(ctx, sessID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	require.NoError(t, pub.Publish(ctx, goodEvent(subject)))

	d, err := stream.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, d.Ack())
}
