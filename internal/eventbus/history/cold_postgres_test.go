// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestColdPostgresUnmarshalsEnvelope asserts that the cold reader
// unmarshals the envelope column to recover all Event proto fields,
// including those (Actor.legacy_id, full Timestamp pb) not present
// as separate columns.
//
// Locks Decision 5: plugin-authored actors carry legacy_id, which is
// recovered via envelope unmarshal — no dedicated column needed.
func TestColdPostgresUnmarshalsEnvelope(t *testing.T) {
	t.Parallel()

	// Build an Event with Actor.legacy_id set (plugin-authored case).
	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.scene.scene-01ABC.start",
		Type:    "core-scenes:scene_started",
		Actor: &eventbusv1.Actor{
			Kind:     eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
			LegacyId: "core-scenes",
		},
		Payload: []byte("{}"),
	}

	envelopeBytes, err := proto.Marshal(envelope)
	require.NoError(t, err)

	// Simulated PG row.
	row := coldRow{
		ID:         envelope.Id,
		Envelope:   envelopeBytes,
		Codec:      string(codec.NameIdentity),
		DEKRef:     sql.NullInt64{Valid: false},
		DEKVersion: sql.NullInt32{Valid: false},
	}

	ev, metaOnly, err := decodeColdRow(context.Background(), row, eventbus.SessionIdentity{}, nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, metaOnly)
	assert.Equal(t, "core-scenes", ev.Actor.LegacyID,
		"legacy_id must be recovered via envelope unmarshal")
	assert.Equal(t, []byte("{}"), ev.Payload)
}
