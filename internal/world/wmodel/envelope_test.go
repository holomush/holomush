// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wmodel_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// TestNewEnvelopeIntentStampsULIDAndCarriesGameID proves the intent constructor
// stamps a fresh event ULID (never hand-minted / idgen) and carries the
// caller-supplied game identity and payload fields — but nothing storage-owned.
func TestNewEnvelopeIntentStampsULIDAndCarriesGameID(t *testing.T) {
	aggID := ulid.Make()
	intent := wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        "main",
		Kind:          "location_updated",
		SchemaVersion: 1,
		Actor:         "character:01ABC",
		CausationID:   "cause-1",
		CorrelationID: "corr-1",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   aggID,
		Payload:       []byte(`{"name":"Atrium"}`),
	})

	assert.NotEqual(t, ulid.ULID{}, intent.EventID, "constructor stamps a fresh event ULID")
	assert.Equal(t, "main", intent.GameID, "game identity is explicit on the intent")
	assert.Equal(t, "location_updated", intent.Kind)
	assert.Equal(t, 1, intent.SchemaVersion)
	assert.Equal(t, "character:01ABC", intent.Actor)
	assert.Equal(t, "cause-1", intent.CausationID)
	assert.Equal(t, "corr-1", intent.CorrelationID)
	assert.Equal(t, wmodel.AggregateLocation, intent.AggregateType)
	assert.Equal(t, aggID, intent.AggregateID)
	assert.Equal(t, []byte(`{"name":"Atrium"}`), intent.Payload)
}

// TestNewEnvelopeIntentMintsDistinctULIDs proves each intent gets its own dedup
// key (the ULID that becomes Nats-Msg-Id).
func TestNewEnvelopeIntentMintsDistinctULIDs(t *testing.T) {
	a := wmodel.NewEnvelopeIntent(wmodel.IntentParams{GameID: "main"})
	b := wmodel.NewEnvelopeIntent(wmodel.IntentParams{GameID: "main"})
	assert.NotEqual(t, a.EventID, b.EventID, "distinct intents carry distinct event ULIDs")
}

// TestFinalizeBuildsManifestFromDeltaAndPreservesGameID proves Finalize is the
// pure constructor that stamps the storage-owned epoch/position, builds the
// affected-aggregates manifest (with before/after versions) from the repo's
// MutationDelta — not command inputs — and carries GameID through unchanged.
func TestFinalizeBuildsManifestFromDeltaAndPreservesGameID(t *testing.T) {
	aggID := ulid.Make()
	cascadeID := ulid.Make()
	intent := wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        "main",
		Kind:          "location_deleted",
		SchemaVersion: 2,
		Actor:         "system",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   aggID,
		Payload:       []byte(`{}`),
	})
	delta := &wmodel.MutationDelta{
		Primary: wmodel.AffectedAggregate{
			Type:          wmodel.AggregateLocation,
			ID:            aggID,
			BeforeVersion: 3,
			AfterVersion:  0,
			Tombstone:     true,
		},
		Affected: []wmodel.AffectedAggregate{
			{Type: wmodel.AggregateExit, ID: cascadeID, Tombstone: true},
		},
	}

	env := wmodel.Finalize(intent, delta, 4, 17)
	require.NotNil(t, env)

	assert.Equal(t, intent.EventID, env.EventID, "event ULID carried through")
	assert.Equal(t, "main", env.GameID, "game identity preserved intent -> envelope")
	assert.Equal(t, "location_deleted", env.Kind)
	assert.Equal(t, 2, env.SchemaVersion)
	assert.Equal(t, int64(4), env.Epoch, "writer-supplied epoch stamped")
	assert.Equal(t, int64(17), env.FeedPosition, "writer-supplied position stamped")

	require.Len(t, env.Affected, 2, "manifest = primary + affected aggregates")
	assert.Equal(t, aggID, env.Affected[0].ID)
	assert.Equal(t, 3, env.Affected[0].BeforeVersion, "before/after versions come from the delta")
	assert.True(t, env.Affected[0].Tombstone)
	assert.Equal(t, cascadeID, env.Affected[1].ID)
	assert.True(t, env.Affected[1].Tombstone)
}
