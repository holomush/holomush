// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestActorKindFromProtoCoversAllKinds exercises every proto actor kind
// mapping including the unspecified/default → Unknown fallback.
func TestActorKindFromProtoCoversAllKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input eventbusv1.ActorKind
		want  ActorKind
	}{
		{"character", eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, ActorKindCharacter},
		{"player", eventbusv1.ActorKind_ACTOR_KIND_PLAYER, ActorKindPlayer},
		{"system", eventbusv1.ActorKind_ACTOR_KIND_SYSTEM, ActorKindSystem},
		{"plugin", eventbusv1.ActorKind_ACTOR_KIND_PLUGIN, ActorKindPlugin},
		{"unspecified → unknown", eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED, ActorKindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, actorKindFromProto(tc.input))
		})
	}
}

func TestActorFromProtoNilReturnsZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Actor{}, actorFromProto(nil))
}

func TestActorFromProtoPopulatesIDWhen16Bytes(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(1, nil)
	a := &eventbusv1.Actor{
		Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
		Id:   id.Bytes(),
	}
	got := actorFromProto(a)
	assert.Equal(t, ActorKindCharacter, got.Kind)
	assert.Equal(t, id, got.ID)
}

func TestActorFromProtoIgnoresWrongSizeID(t *testing.T) {
	t.Parallel()
	a := &eventbusv1.Actor{
		Kind: eventbusv1.ActorKind_ACTOR_KIND_PLAYER,
		Id:   []byte{0xFF, 0xFF, 0xFF}, // not 16 bytes
	}
	got := actorFromProto(a)
	assert.Equal(t, ActorKindPlayer, got.Kind)
	var zero ulid.ULID
	assert.Equal(t, zero, got.ID)
}

func TestSubjectsToStringsEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, subjectsToStrings(nil))
	assert.Nil(t, subjectsToStrings([]Subject{}))
}

func TestSubjectsToStringsConvertsAll(t *testing.T) {
	t.Parallel()
	in := []Subject{"events.main.a", "events.main.b"}
	out := subjectsToStrings(in)
	assert.Equal(t, []string{"events.main.a", "events.main.b"}, out)
}
