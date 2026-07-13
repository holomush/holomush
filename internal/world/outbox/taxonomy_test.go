// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/outbox"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestLookupDeclaredKindReturnsSchemaAndVersion proves a declared world-change
// kind resolves to its per-type payload schema and a schema version — the
// versioned taxonomy contract the rollout (05-10/05-11) wires each command to.
func TestLookupDeclaredKindReturnsSchemaAndVersion(t *testing.T) {
	schema, err := outbox.Lookup(outbox.KindCharacterMoved)
	require.NoError(t, err)
	assert.Equal(t, outbox.KindCharacterMoved, schema.Kind)
	assert.Equal(t, wmodel.AggregateCharacter, schema.Aggregate)
	assert.GreaterOrEqual(t, schema.SchemaVersion, 1,
		"every declared kind carries an App-Schema-Version >= 1")
	assert.NotEmpty(t, schema.Payload, "a declared kind describes its payload schema")
}

// TestLookupUndeclaredKindIsRejected proves an undeclared kind is REJECTED at
// lookup (not silently accepted) — the enforcement the census (05-11) leans on.
func TestLookupUndeclaredKindIsRejected(t *testing.T) {
	_, err := outbox.Lookup("totally_made_up_kind")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "WORLD_TAXONOMY_UNKNOWN_KIND")

	assert.False(t, outbox.IsDeclared("totally_made_up_kind"))
	assert.True(t, outbox.IsDeclared(outbox.KindCharacterMoved))
}

// TestEveryDeclaredKindCarriesAppSchemaVersion proves the registry is versioned:
// every declared kind carries a positive App-Schema-Version and a non-empty
// aggregate + payload schema (the ARCH-04 / Phase-7 self-describing input).
func TestEveryDeclaredKindCarriesAppSchemaVersion(t *testing.T) {
	kinds := outbox.Kinds()
	require.NotEmpty(t, kinds)
	assert.GreaterOrEqual(t, outbox.AppSchemaVersion, 1, "package App-Schema-Version is positive")

	for _, kind := range kinds {
		schema, err := outbox.Lookup(kind)
		require.NoError(t, err, "declared kind %q must resolve", kind)
		assert.GreaterOrEqual(t, schema.SchemaVersion, 1, "kind %q carries a schema version >= 1", kind)
		assert.NotEmpty(t, schema.Aggregate, "kind %q names its aggregate", kind)
		assert.NotEmpty(t, schema.Payload, "kind %q describes its payload schema", kind)
	}
}

// TestExamineKindsAreAbsent proves examine (a READ) is intentionally excluded
// from the world-change taxonomy — the feed carries state changes only (RESEARCH
// Open Question 1).
func TestExamineKindsAreAbsent(t *testing.T) {
	for _, kind := range outbox.Kinds() {
		assert.NotContains(t, kind, "examine", "examine is a read, never a world-change kind")
	}
	_, err := outbox.Lookup("character_examined")
	assert.Error(t, err, "an examine kind must not be declared")
}

// TestNoSceneParticipantKind proves the vestigial world scene-participant write
// surface (removed in 05-14, D-07) has NO declared kind — resolving the
// D-01<->D-05 contradiction by removal.
func TestNoSceneParticipantKind(t *testing.T) {
	for _, kind := range outbox.Kinds() {
		assert.NotContains(t, kind, "participant", "no scene-participant kind is declared (D-07)")
		assert.NotContains(t, kind, "scene", "no scene kind is declared in the world taxonomy (D-07)")
	}
}

// TestCharacterGenesisKindExists proves CreateCharacter has a character-genesis
// kind (Open Question 3) — its emitting site is the atomic character-genesis
// service (05-15) covering all three creation paths.
func TestCharacterGenesisKindExists(t *testing.T) {
	schema, err := outbox.Lookup(outbox.KindCharacterGenesis)
	require.NoError(t, err)
	assert.Equal(t, wmodel.AggregateCharacter, schema.Aggregate)
}

// TestCharacterDeleteKindIsTombstone proves the single character delete/tombstone
// kind reused by DeleteCharacter (05-11) and the guest reaper (05-16, D-06).
func TestCharacterDeleteKindIsTombstone(t *testing.T) {
	schema, err := outbox.Lookup(outbox.KindCharacterDeleted)
	require.NoError(t, err)
	assert.True(t, schema.Tombstone, "character delete is a tombstone kind")
}

// TestCharacterPreferencesUpdateKindExists proves the folded-in character-settings
// write (round-4 C5 / D-05, Task 2) has a declared kind.
func TestCharacterPreferencesUpdateKindExists(t *testing.T) {
	schema, err := outbox.Lookup(outbox.KindCharacterPreferencesUpdate)
	require.NoError(t, err)
	assert.Equal(t, wmodel.AggregateCharacter, schema.Aggregate)
	assert.False(t, schema.Tombstone)
}

// TestRegistryDeclaresWorldChangeKinds proves the create/update/delete/move
// per-aggregate vocabulary is declared for the four core world aggregates.
func TestRegistryDeclaresWorldChangeKinds(t *testing.T) {
	want := []string{
		outbox.KindLocationCreated, outbox.KindLocationUpdated, outbox.KindLocationDeleted,
		outbox.KindExitCreated, outbox.KindExitUpdated, outbox.KindExitDeleted,
		outbox.KindObjectCreated, outbox.KindObjectUpdated, outbox.KindObjectDeleted, outbox.KindObjectMoved,
		outbox.KindCharacterGenesis, outbox.KindCharacterUpdated, outbox.KindCharacterDeleted,
		outbox.KindCharacterMoved, outbox.KindCharacterPreferencesUpdate,
	}
	for _, kind := range want {
		require.True(t, outbox.IsDeclared(kind), "kind %q must be declared", kind)
		assert.False(t, strings.HasPrefix(kind, "scene"), "no scene kinds (D-07)")
	}
}
