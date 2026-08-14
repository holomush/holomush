// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox_test

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
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

// --- 06-05 Task 1: the eight-step taxonomy ratchet (D-103 / D-105) ---

// payloadFieldNames returns the declared field names of a kind's payload
// schema, so an assertion is made against the DECLARATION rather than against a
// struct that happens to marshal the same way.
func payloadFieldNames(t *testing.T, kind string) []string {
	t.Helper()
	schema, err := outbox.Lookup(kind)
	require.NoError(t, err)
	require.NotEmpty(t, schema.Payload, "a declared kind describes its payload schema")
	names := make([]string, 0, len(schema.Payload))
	for _, f := range schema.Payload {
		names = append(names, f.Name)
	}
	return names
}

// TestCharacterLifecyclePayloadDeclaresBeforeStatusSectionAndAction pins steps
// 4 and 5 of the ratchet: the widened lifecycle payload shape AND the
// SchemaVersion bump that declares it, on BOTH lifecycle kinds.
//
// Reverting either kind's SchemaVersion to 1 fails this test — which is what
// makes the all-or-none claim true rather than asserted.
func TestCharacterLifecyclePayloadDeclaresBeforeStatusSectionAndAction(t *testing.T) {
	for _, kind := range []string{outbox.KindCharacterRetired, outbox.KindCharacterUnretired} {
		t.Run(kind, func(t *testing.T) {
			assert.ElementsMatch(t,
				[]string{"character_id", "status", "before_status", "section", "action"},
				payloadFieldNames(t, kind),
				"the lifecycle payload declares the D-103 before-status and the §10.7 admin context")

			schema, err := outbox.Lookup(kind)
			require.NoError(t, err)
			assert.Equal(t, 2, schema.SchemaVersion,
				"widening a declared payload REQUIRES its per-kind schema version to move 1 -> 2")
		})
	}
}

// TestCharacterProfilePayloadDeclaresSectionAndAction pins steps 6 and 7 — the
// half an earlier six-step enumeration left able to drift green.
//
// The admin profile write REUSES KindCharacterProfileUpdate rather than minting
// an admin kind, so this one declaration carries both actor flavours; the player
// path emits empty section and action on it, which is correct because the
// envelope Actor already distinguishes who acted.
func TestCharacterProfilePayloadDeclaresSectionAndAction(t *testing.T) {
	assert.ElementsMatch(t,
		[]string{"character_id", "changed_attributes", "section", "action"},
		payloadFieldNames(t, outbox.KindCharacterProfileUpdate),
		"the profile payload gains the §10.7 admin context and still carries NO values")

	schema, err := outbox.Lookup(outbox.KindCharacterProfileUpdate)
	require.NoError(t, err)
	assert.Equal(t, 2, schema.SchemaVersion,
		"widening characterProfilePayload REQUIRES KindCharacterProfileUpdate's schema version to move 1 -> 2")
}

// TestAppSchemaVersionTracksTheWidenedCharacterPayloads pins step 8, the
// registry-revision bump AppSchemaVersion's own doc comment requires whenever a
// per-type payload schema changes.
//
// It is a separate test from the two above precisely so reverting the
// AppSchemaVersion bump ALONE — with both payload widenings and all three
// per-kind bumps left in place — still fails.
func TestAppSchemaVersionTracksTheWidenedCharacterPayloads(t *testing.T) {
	assert.Equal(t, 4, outbox.AppSchemaVersion,
		"revision 4 widens both character lifecycle payloads and the character profile payload (v0.13 phase-06 plan 05)")
}

// TestNoAdminOnlyCharacterKindWasMinted proves the reuse decision structurally:
// the admin profile write and the admin lifecycle transitions declare the SAME
// three kinds the player path declares, and no fourth admin-flavoured character
// kind exists.
//
// A distinct admin kind would force a census row, a command->kind parity row and
// a service kind-list entry — none of which this plan touches — and would split
// "a character's profile changed" into two audit vocabularies keyed on who
// acted, which the envelope Actor already records.
func TestNoAdminOnlyCharacterKindWasMinted(t *testing.T) {
	for _, kind := range outbox.Kinds() {
		assert.NotContains(t, strings.ToLower(kind), "admin",
			"the admin write reuses the shipped character kinds; %q looks like a minted admin kind", kind)
	}
}

// TestARelayedWorldEnvelopeCarriesNoRenderingMetadata pins the CURRENT boundary
// between the world outbox and the host audit projection — a boundary this
// phase's D-105 reasoning assumes is closed and which is not.
//
// # What is true
//
// An admin mutation's audit envelope commits or rolls back WITH its state change.
// That is proven end to end in test/integration/access/admin_characters_write_test.go
// and it is the half of ROADMAP criterion 3 that holds.
//
// # What is NOT true today
//
// The second half — "the events_audit row is PROJECTED from that envelope" — does
// not happen for a world-outbox envelope, because the two ends do not meet:
//
//   - EnvelopeToEvent (wire.go) constructs the eventbus.Event with no Rendering,
//     asserted below.
//   - The relay publishes through EventBus.Publisher() — a bare
//     JetStreamPublisher. Only eventbus.RenderingPublisher writes the
//     App-Rendering header, and it is not in the relay's path. (It could not be:
//     its Lookup resolves the wire type against plugin verbs[].type and
//     hard-fails EMIT_UNKNOWN_VERB on a world-change kind like character_retired.)
//   - audit.writeAuditRow REQUIRES App-Rendering and returns AUDIT_MISSING_HEADER
//     without it (projection.go, the renderingJSON == "" arm).
//
// So a world envelope reaching the projection is rejected rather than persisted.
// This test asserts the first bullet — the one fact that lives in this package —
// so the boundary is recorded as a pinned property rather than as a comment. If a
// future change gives relayed world events rendering metadata, this test goes RED
// and whoever does it is pointed at the audit-projection contract that change
// would newly satisfy.
func TestARelayedWorldEnvelopeCarriesNoRenderingMetadata(t *testing.T) {
	env := wmodel.Envelope{
		EventID:       ulid.Make(),
		GameID:        "main",
		Kind:          outbox.KindCharacterRetired,
		SchemaVersion: 2,
		Actor:         "player:" + ulid.Make().String(),
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   ulid.Make(),
		Payload:       []byte(`{"character_id":"x","status":"retired","before_status":"active","section":"characters","action":"write"}`),
	}

	ev, err := outbox.EnvelopeToEvent(env)
	require.NoError(t, err)
	require.NotEmpty(t, ev.Payload, "control: the adapter really did build an event")
	assert.Nil(t, ev.Rendering,
		"a relayed world envelope carries NO rendering metadata, so the host audit projection "+
			"(which requires the App-Rendering header) cannot persist it — see this test's doc comment")
	assert.NotContains(t, ev.Headers, "App-Rendering",
		"and the header is absent too: only eventbus.RenderingPublisher writes it, and the relay "+
			"does not route through it")
}
