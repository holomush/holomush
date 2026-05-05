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
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestColdPostgresUnmarshalsEnvelope asserts that the cold reader
// unmarshals the envelope column to recover all Event proto fields,
// including the Actor ULID and Payload — proving the cold tier
// recovers actor identity via envelope unmarshal rather than
// requiring a dedicated column.
func TestColdPostgresUnmarshalsEnvelope(t *testing.T) {
	t.Parallel()

	actorID := makeULIDBytes(t)
	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.scene.scene-01ABC.start",
		Type:    "core-scenes:scene_started",
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
			Id:   actorID,
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
	assert.Equal(t, actorID, ev.Actor.ID.Bytes(),
		"actor ULID must be recovered via envelope unmarshal")
	assert.Equal(t, []byte("{}"), ev.Payload)
}

// TestColdPostgresRejectsSensitiveRowMissingDEKColumns locks the
// fail-closed contract for sensitive (non-identity) rows missing
// dek_ref / dek_version columns. Mirrors the hot-tier
// EVENTBUS_HISTORY_DEK_HEADER_MISSING contract violation. Without this
// guard, a corrupted or partially-projected sensitive row would silently
// pass (keyID=0, keyVersion=0) to the dispatcher and surface as a
// confusing Resolve(0,0) miss or auth-guard mismatch.
//
// Refs: CodeRabbit Pass 2 finding 2026-05-04 (PR #3521).
func TestColdPostgresRejectsSensitiveRowMissingDEKColumns(t *testing.T) {
	t.Parallel()

	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.scene.scene-01XYZ.private",
		Type:    "core-scenes:secret",
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
			Id:   makeULIDBytes(t),
		},
		Payload: []byte("ciphertext-bytes"),
	}
	envelopeBytes, err := proto.Marshal(envelope)
	require.NoError(t, err)

	// Sensitive codec name with NULL DEK columns — corruption case.
	row := coldRow{
		ID:         envelope.Id,
		Envelope:   envelopeBytes,
		Codec:      "xchacha20poly1305-v1",
		DEKRef:     sql.NullInt64{Valid: false},
		DEKVersion: sql.NullInt32{Valid: false},
	}

	_, _, err = decodeColdRow(context.Background(), row, eventbus.SessionIdentity{}, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_COLD_DEK_COLUMNS_MISSING")
}

// TestColdPostgresRejectsSensitiveRowNegativeDEKValues locks the
// rejection of negative DEK column values on sensitive rows. PostgreSQL
// BIGSERIAL / INTEGER columns are non-negative by contract; a negative
// value indicates corruption that fail-closed handling MUST surface.
func TestColdPostgresRejectsSensitiveRowNegativeDEKValues(t *testing.T) {
	t.Parallel()

	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.scene.scene-01ABC.private",
		Type:    "core-scenes:secret",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER},
		Payload: []byte("ciphertext-bytes"),
	}
	envelopeBytes, err := proto.Marshal(envelope)
	require.NoError(t, err)

	row := coldRow{
		ID:         envelope.Id,
		Envelope:   envelopeBytes,
		Codec:      "xchacha20poly1305-v1",
		DEKRef:     sql.NullInt64{Valid: true, Int64: -1},
		DEKVersion: sql.NullInt32{Valid: true, Int32: 0},
	}

	_, _, err = decodeColdRow(context.Background(), row, eventbus.SessionIdentity{}, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_COLD_BAD_DEK_COLUMNS")
}
