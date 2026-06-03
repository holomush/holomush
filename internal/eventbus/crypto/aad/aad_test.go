// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package aad_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// newTestEvent returns a fully-populated Event for AAD tests. Modify
// fields in copies to test field-level tampering.
func newTestEvent() *eventbusv1.Event {
	return &eventbusv1.Event{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.game-1.scene.01ABC.ic",
		Type:      "core-communication:whisper",
		Timestamp: timestamppb.New(timeFromUnixNano(1714501234567890123)),
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("01HXXX0000000000"),
		},
	}
}

func TestBuild_StartsWithMagicVersionPrefix(t *testing.T) {
	out, err := aad.Build(newTestEvent(), "xchacha20poly1305-v1", 42, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out), 6)
	assert.Equal(t, []byte("HMAAD\x01"), out[:6])
}

func TestBuild_DeterministicForIdenticalInputs(t *testing.T) {
	e := newTestEvent()
	a, err := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
	require.NoError(t, err)
	b, err := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
	require.NoError(t, err)
	assert.Equal(t, a, b, "Build must be deterministic for identical inputs")
}

func TestBuild_AnyFieldChange_ChangesOutput(t *testing.T) {
	base := newTestEvent()
	baseAAD, err := aad.Build(base, "xchacha20poly1305-v1", 42, 1)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(e *eventbusv1.Event) (newCodec string, newDekRef uint64, newDekVer uint32)
	}{
		{"id changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Id = []byte("FFFFFFFFFFFFFFFF")
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"subject changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Subject = "events.game-1.scene.01ABC.ooc"
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"type changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Type = "core-communication:say"
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"timestamp changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Timestamp = timestamppb.New(timeFromUnixNano(1714501234567890124))
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"actor id changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Actor.Id = []byte("01HYYY0000000000")
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"actor kind changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
			e.Actor.Kind = eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
			return "xchacha20poly1305-v1", 42, 1
		}},
		{"codec name changed", func(_ *eventbusv1.Event) (string, uint64, uint32) {
			return "aes-gcm-v1", 42, 1
		}},
		{"dek ref changed", func(_ *eventbusv1.Event) (string, uint64, uint32) {
			return "xchacha20poly1305-v1", 43, 1
		}},
		{"dek version changed", func(_ *eventbusv1.Event) (string, uint64, uint32) {
			return "xchacha20poly1305-v1", 42, 2
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := newTestEvent()
			codec, dekRef, dekVer := tt.mutate(mutated)
			mutatedAAD, err := aad.Build(mutated, codec, dekRef, dekVer)
			require.NoError(t, err)
			assert.NotEqual(t, baseAAD, mutatedAAD,
				"tampering with %s must change AAD output", tt.name)
		})
	}
}

func TestBuild_ActorMarshalIsDeterministic(t *testing.T) {
	// Verifies the Actor proto submessage is canonicalized via
	// proto.MarshalOptions{Deterministic: true} — bare proto.Marshal
	// would silently produce non-byte-equal AAD across runs and break
	// INV-CRYPTO-14.
	e := newTestEvent()

	first, err := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
	require.NoError(t, err)
	for i := 0; i < 1000; i++ {
		next, err := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
		require.NoError(t, err)
		require.Equal(t, first, next,
			"iteration %d produced different AAD bytes — Actor marshal not deterministic", i)
	}
}

func TestBuild_DekRefZero_ForIdentityCodec(t *testing.T) {
	// Cleartext events use codec=identity with no DEK. The function
	// should accept dekRef=0, dekVersion=0 and produce well-formed AAD.
	e := newTestEvent()
	out, err := aad.Build(e, "identity", 0, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out), 6, "output too short to contain magic prefix")
	assert.Equal(t, []byte("HMAAD\x01"), out[:6])
}

func timeFromUnixNano(ns int64) time.Time { return time.Unix(0, ns).UTC() }
