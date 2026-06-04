// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// ---- test fakes ----

type fakeDEKResolver struct {
	resolveFn func(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)
}

func (f *fakeDEKResolver) Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error) {
	return f.resolveFn(ctx, keyID, version)
}

type fakeCodecResolver struct {
	resolveFn func(name codec.Name) (codec.Codec, error)
}

func (f *fakeCodecResolver) Resolve(name codec.Name) (codec.Codec, error) {
	return f.resolveFn(name)
}

// realCodecResolver wraps the package-level codec.Resolve for production-shape tests.
type realCodecResolver struct{}

func (realCodecResolver) Resolve(name codec.Name) (codec.Codec, error) {
	return codec.Resolve(name)
}

// buildEncryptedColdRow constructs a ColdRow whose Envelope contains a real
// xchacha20poly1305-v1 ciphertext of plaintext, AAD-bound to the envelope fields.
// The returned codec.Key is the DEK used for encryption.
func buildEncryptedColdRow(t *testing.T, plaintext []byte) (readstream.ColdRow, codec.Key) {
	t.Helper()

	const (
		dekRef     = uint64(1)
		dekVersion = uint32(1)
	)
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = 0xAB // deterministic test key
	}
	testKey := codec.Key{ID: 1, Version: 1, Bytes: keyBytes}

	// Build the proto envelope — payload starts empty (AAD uses cleartext fields).
	envProto := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000A"),
		Subject: "scene.test.subject",
		Type:    "scene.pose",
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("01JDTEST00000000000B"),
		},
	}

	// Build AAD from cleartext envelope fields.
	aadBytes, err := aad.Build(envProto, string(codec.NameXChaCha20v1), dekRef, dekVersion)
	require.NoError(t, err)

	// Encrypt.
	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), plaintext, testKey, aadBytes)
	require.NoError(t, err)

	// Store ciphertext in payload; marshal full envelope.
	envProto.Payload = ciphertext
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envProto)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   envelopeBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(dekRef),
		KeyVersion: dekVersion,
	}
	return row, testKey
}

// ---- INV-CRYPTO-62 classifier matrix ----

func TestINV_CRYPTO_62_ClassifierMatrix(t *testing.T) {
	// We test classifyDecryptErr indirectly through DecryptRow by injecting
	// errors at the DEK-resolve step and checking the (reason, fatal) output.
	tests := []struct {
		name       string
		injectErr  error
		wantReason eventbus.NoPlaintextReason
		wantFatal  bool
		wantErrNil bool
	}{
		{
			name:       "nil error (success branch — tested via happy path)",
			injectErr:  nil, // not injected; placeholder
			wantReason: eventbus.NoPlaintextReasonUnspecified,
			wantFatal:  false,
			wantErrNil: true,
		},
		{
			name:       "context.Canceled → fatal=true, UNSPECIFIED",
			injectErr:  context.Canceled,
			wantReason: eventbus.NoPlaintextReasonUnspecified,
			wantFatal:  true,
		},
		{
			name:       "context.DeadlineExceeded → fatal=true, UNSPECIFIED",
			injectErr:  context.DeadlineExceeded,
			wantReason: eventbus.NoPlaintextReasonUnspecified,
			wantFatal:  true,
		},
		{
			name:       "source.ErrMetadataOnly → STALE_DEK, fatal=false",
			injectErr:  source.ErrMetadataOnly,
			wantReason: eventbus.NoPlaintextReasonStaleDEK,
			wantFatal:  false,
		},
		{
			name:       "oops DEK_NOT_FOUND → STALE_DEK, fatal=false",
			injectErr:  oops.Code("DEK_NOT_FOUND").Errorf("key gone"),
			wantReason: eventbus.NoPlaintextReasonStaleDEK,
			wantFatal:  false,
		},
		{
			name:       "oops DEK_DESTROYED → STALE_DEK, fatal=false",
			injectErr:  oops.Code("DEK_DESTROYED").Errorf("rekey'd"),
			wantReason: eventbus.NoPlaintextReasonStaleDEK,
			wantFatal:  false,
		},
		{
			name:       "generic error → INTERNAL, fatal=false",
			injectErr:  errors.New("unexpected db failure"),
			wantReason: eventbus.NoPlaintextReasonInternal,
			wantFatal:  false,
		},
		// Branch 5 additions: malformed DEK columns → DEKBadColumns (Finding 1, R.20).
		{
			name:       "ADMIN_READSTREAM_COLD_DEK_VERSION_NULL → DEK_BAD_COLUMNS, fatal=false",
			injectErr:  oops.Code("ADMIN_READSTREAM_COLD_DEK_VERSION_NULL").Errorf("dek_version NULL with dek_ref present"),
			wantReason: eventbus.NoPlaintextReasonDEKBadColumns,
			wantFatal:  false,
		},
		{
			name:       "ADMIN_READSTREAM_COLD_NO_DEK → DEK_BAD_COLUMNS, fatal=false",
			injectErr:  oops.Code("ADMIN_READSTREAM_COLD_NO_DEK").Errorf("row has no dek_ref"),
			wantReason: eventbus.NoPlaintextReasonDEKBadColumns,
			wantFatal:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErrNil {
				// nil-error branch tested by happy-path test; skip here.
				t.Skip("nil error branch covered by TestDecryptRow_HappyPath")
			}

			// We need a valid marshaled envelope so DecryptRow reaches the DEK-resolve step.
			envProto := &eventbusv1.Event{
				Id:      []byte("01JDTEST00000000000C"),
				Subject: "scene.test",
				Type:    "scene.pose",
				Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
			}
			envelopeBytes, err := proto.Marshal(envProto)
			require.NoError(t, err)

			row := readstream.ColdRow{
				Envelope:   envelopeBytes,
				Codec:      codec.NameXChaCha20v1, // non-identity to trigger DEK resolve
				KeyID:      codec.KeyID(1),
				KeyVersion: 1,
			}

			dekResolver := &fakeDEKResolver{
				resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
					return codec.Key{}, tt.injectErr
				},
			}

			plaintext, reason, fatal, gotErr := readstream.DecryptRow(
				context.Background(), row, dekResolver, realCodecResolver{},
			)

			assert.Nil(t, plaintext)
			assert.Equal(t, tt.wantReason, reason, "reason mismatch for %s", tt.name)
			assert.Equal(t, tt.wantFatal, fatal, "fatal mismatch for %s", tt.name)
			require.Error(t, gotErr)
		})
	}
}

// ---- Happy path ----

func TestDecryptRow_HappyPath(t *testing.T) {
	plaintext := []byte(`{"scene":"test payload"}`)
	row, testKey := buildEncryptedColdRow(t, plaintext)

	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return testKey, nil
		},
	}

	got, reason, fatal, err := readstream.DecryptRow(
		context.Background(), row, dekResolver, realCodecResolver{},
	)

	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
	assert.Equal(t, eventbus.NoPlaintextReasonUnspecified, reason)
	assert.False(t, fatal)
}

// ---- DEK error paths ----

func TestDecryptRow_DEKDestroyed(t *testing.T) {
	envProto := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000D"),
		Subject: "scene.test",
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}
	envelopeBytes, err := proto.Marshal(envProto)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   envelopeBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(42),
		KeyVersion: 1,
	}

	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("rekey'd")
		},
	}

	plaintext, reason, fatal, gotErr := readstream.DecryptRow(
		context.Background(), row, dekResolver, realCodecResolver{},
	)

	assert.Nil(t, plaintext)
	assert.Equal(t, eventbus.NoPlaintextReasonStaleDEK, reason)
	assert.False(t, fatal)
	require.Error(t, gotErr)
}

func TestDecryptRow_DEKNotFound(t *testing.T) {
	envProto := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000E"),
		Subject: "scene.test",
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}
	envelopeBytes, err := proto.Marshal(envProto)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   envelopeBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(99),
		KeyVersion: 1,
	}

	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return codec.Key{}, oops.Code("DEK_NOT_FOUND").Errorf("orphan ref")
		},
	}

	plaintext, reason, fatal, gotErr := readstream.DecryptRow(
		context.Background(), row, dekResolver, realCodecResolver{},
	)

	assert.Nil(t, plaintext)
	assert.Equal(t, eventbus.NoPlaintextReasonStaleDEK, reason)
	assert.False(t, fatal)
	require.Error(t, gotErr)
}

// ---- AAD tamper path ----

func TestDecryptRow_AADTampered(t *testing.T) {
	// Encrypt with subject "original.subject", then hand DecryptRow an envelope
	// whose subject has been changed — the AAD will differ, causing Open to fail.
	const dekRef = uint64(1)
	const dekVersion = uint32(1)
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = 0xCD
	}
	testKey := codec.Key{ID: 1, Version: 1, Bytes: keyBytes}

	// Encrypt with the original subject.
	origEnv := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000F"),
		Subject: "original.subject",
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}
	origAAD, err := aad.Build(origEnv, string(codec.NameXChaCha20v1), dekRef, dekVersion)
	require.NoError(t, err)

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), []byte("secret"), testKey, origAAD)
	require.NoError(t, err)

	// Tamper: change the subject in the envelope.
	tamperedEnv := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000F"),
		Subject: "tampered.subject", // different — AAD mismatch
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
		Payload: ciphertext,
	}
	tamperedBytes, err := proto.Marshal(tamperedEnv)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   tamperedBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(dekRef),
		KeyVersion: dekVersion,
	}
	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return testKey, nil
		},
	}

	plaintext, reason, fatal, gotErr := readstream.DecryptRow(
		context.Background(), row, dekResolver, realCodecResolver{},
	)

	assert.Nil(t, plaintext)
	assert.Equal(t, eventbus.NoPlaintextReasonInternal, reason)
	assert.False(t, fatal)
	require.Error(t, gotErr)
}

// ---- Context cancellation ----

func TestDecryptRow_ContextCanceled(t *testing.T) {
	envProto := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000G"),
		Subject: "scene.test",
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}
	envelopeBytes, err := proto.Marshal(envProto)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   envelopeBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(1),
		KeyVersion: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return codec.Key{}, context.Canceled
		},
	}

	plaintext, reason, fatal, gotErr := readstream.DecryptRow(
		ctx, row, dekResolver, realCodecResolver{},
	)

	assert.Nil(t, plaintext)
	assert.Equal(t, eventbus.NoPlaintextReasonUnspecified, reason)
	assert.True(t, fatal, "context.Canceled must set fatal=true")
	require.Error(t, gotErr)
}

// ---- Codec resolve failure ----

func TestDecryptRow_CodecResolveFails(t *testing.T) {
	envProto := &eventbusv1.Event{
		Id:      []byte("01JDTEST00000000000H"),
		Subject: "scene.test",
		Type:    "scene.pose",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}
	envelopeBytes, err := proto.Marshal(envProto)
	require.NoError(t, err)

	row := readstream.ColdRow{
		Envelope:   envelopeBytes,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      codec.KeyID(1),
		KeyVersion: 1,
	}

	keyBytes := make([]byte, 32)
	testKey := codec.Key{ID: 1, Version: 1, Bytes: keyBytes}

	dekResolver := &fakeDEKResolver{
		resolveFn: func(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
			return testKey, nil
		},
	}
	codecResolver := &fakeCodecResolver{
		resolveFn: func(_ codec.Name) (codec.Codec, error) {
			return nil, errors.New("codec registry: no such codec")
		},
	}

	plaintext, reason, fatal, gotErr := readstream.DecryptRow(
		context.Background(), row, dekResolver, codecResolver,
	)

	assert.Nil(t, plaintext)
	assert.Equal(t, eventbus.NoPlaintextReasonInternal, reason)
	assert.False(t, fatal)
	require.Error(t, gotErr)
}
