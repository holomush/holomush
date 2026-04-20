// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestActorFromEnvelopeNilReturnsZero covers the nil-input guard.
func TestActorFromEnvelopeNilReturnsZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, eventbus.Actor{}, actorFromEnvelope(nil))
}

func TestActorFromEnvelopeAllKinds(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(1, nil)
	tests := []struct {
		name  string
		proto eventbusv1.ActorKind
		want  eventbus.ActorKind
	}{
		{"character", eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, eventbus.ActorKindCharacter},
		{"player", eventbusv1.ActorKind_ACTOR_KIND_PLAYER, eventbus.ActorKindPlayer},
		{"system", eventbusv1.ActorKind_ACTOR_KIND_SYSTEM, eventbus.ActorKindSystem},
		{"plugin", eventbusv1.ActorKind_ACTOR_KIND_PLUGIN, eventbus.ActorKindPlugin},
		{"unspecified → unknown", eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED, eventbus.ActorKindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &eventbusv1.Actor{Kind: tc.proto, Id: id.Bytes()}
			got := actorFromEnvelope(a)
			assert.Equal(t, tc.want, got.Kind)
			assert.Equal(t, id, got.ID)
		})
	}
}

func TestActorFromEnvelopeFallsBackToLegacyID(t *testing.T) {
	t.Parallel()
	// No binary Id (len != 16), but LegacyID set — caller populates
	// LegacyID instead.
	a := &eventbusv1.Actor{
		Kind:     eventbusv1.ActorKind_ACTOR_KIND_SYSTEM,
		LegacyId: "legacy-actor-1",
	}
	got := actorFromEnvelope(a)
	assert.Equal(t, eventbus.ActorKindSystem, got.Kind)
	assert.Equal(t, "legacy-actor-1", got.LegacyID)
}

func TestActorFromEnvelopeEmptyIDLeavesBothBlank(t *testing.T) {
	t.Parallel()
	a := &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER}
	got := actorFromEnvelope(a)
	assert.Equal(t, eventbus.ActorKindCharacter, got.Kind)
	var zero ulid.ULID
	assert.Equal(t, zero, got.ID)
	assert.Empty(t, got.LegacyID)
}
