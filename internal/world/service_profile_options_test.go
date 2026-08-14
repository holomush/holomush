// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// The three ProfileUpdateOptions the ADMIN write surface supplies, and the
// player-path controls that prove supplying none leaves the shipped behaviour
// byte-identical.
//
// UpdateCharacterProfileAttributes is extended rather than duplicated because
// 01-SPEC §10.6's admin mask is `description` PLUS the twelve `profile.*` names,
// while this method's closed §7.2 name set REJECTS `description` and
// `description` has its own domain command. Two domain calls would each bump
// characters.version, so one request's single expected_version could not fund
// both: the second fails its precheck, one RPC emits two envelopes in two
// transactions, and a partial commit is reachable.
//
// Worse, UpdateCharacterDescription emits kindCharacterUpdated, whose declared
// payload carries a `description` STRING — the prose VALUE. Routing an admin
// description edit through it would write player-authored prose into the
// RETAINED events_audit, which D-103 forbids.

// profileUpdateEnvelope returns the single emitted envelope's decoded payload,
// failing if the count is anything but one.
func profileUpdateEnvelope(t *testing.T, outbox *profileTxOutbox) world.CharacterProfileUpdateChangePayload {
	t.Helper()
	require.Len(t, outbox.rows, 1, "one request is ONE envelope")
	return decodeProfileChangePayload(t, outbox.rows[0])
}

// TestUpdateCharacterProfileAttributesWithDescriptionIsOneWrite pins the R-28
// combined write: a mask naming `description` alongside a `profile.*` path is
// ONE domain write, ONE version bump, ONE transaction and ONE envelope.
func TestUpdateCharacterProfileAttributesWithDescriptionIsOneWrite(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
	seedProfileRow(props, charID, "profile.biography", "Born under a red sky.")

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive, Description: "a tall figure"}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	var written *world.Character
	mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
		Run(func(_ context.Context, c *world.Character) { written = c }).
		Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
			Type: wmodel.AggregateCharacter, ID: charID, BeforeVersion: 5, AfterVersion: 6,
		}}, nil)

	err := svc.UpdateCharacterProfileAttributes(
		ctx, world.HumanCaller(subjectID), charID, 5,
		map[string]string{"profile.biography": "Born under a green sky."},
		world.WithDescription("a short figure"),
		world.WithAuditContext(world.AuditContext{Section: "characters", Action: "write"}),
	)
	require.NoError(t, err)

	require.NotNil(t, written, "the character row is written exactly once, carrying the new description")
	assert.Equal(t, "a short figure", written.Description)
	assert.Equal(t, 5, written.Version, "the CAS carries the CALLER's expected version")

	row, ok := props.lookup("character", charID, "profile.biography")
	require.True(t, ok)
	require.NotNil(t, row.Value)
	assert.Equal(t, "Born under a green sky.", *row.Value)

	got := profileUpdateEnvelope(t, outbox)
	assert.Equal(t, []string{"description", "profile.biography"}, got.ChangedAttributes,
		"description travels as a NAME in the sorted changed list")
	assert.Equal(t, "characters", got.Section)
	assert.Equal(t, "write", got.Action)

	// The prose-absence proof at the layer where values actually flow.
	raw := string(outbox.rows[0].Payload)
	assert.NotContains(t, raw, "a tall figure", "the OLD description value MUST NOT reach the payload bytes")
	assert.NotContains(t, raw, "a short figure", "the NEW description value MUST NOT reach the payload bytes")
	assert.NotContains(t, raw, "red sky", "the OLD profile value MUST NOT reach the payload bytes")
	assert.NotContains(t, raw, "green sky", "the NEW profile value MUST NOT reach the payload bytes")
	assert.Equal(t, "character_profile_update", outbox.rows[0].Kind,
		"never kindCharacterUpdated, whose declared payload carries the description STRING")
}

// TestUpdateCharacterProfileAttributesDescriptionOnlyMaskDefeatsTheEmptyPartitionReturn
// is the C2-26 case: a description-only mask puts NOTHING in attributes, so all
// three partitions are empty and the shipped early return would fire and drop
// the description silently — for exactly the mask the combined-write fix exists
// to serve.
func TestUpdateCharacterProfileAttributesDescriptionOnlyMaskDefeatsTheEmptyPartitionReturn(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, _, outbox := profileTxFixture(t, subjectID, charID)
	stored := &world.Character{ID: charID, Name: "Alice", Version: 3, Status: world.StatusActive, Description: "before"}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	var written *world.Character
	mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
		Run(func(_ context.Context, c *world.Character) { written = c }).
		Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
			Type: wmodel.AggregateCharacter, ID: charID, BeforeVersion: 3, AfterVersion: 4,
		}}, nil)

	err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3,
		map[string]string{}, world.WithDescription("after"))
	require.NoError(t, err)

	require.NotNil(t, written, "the description write MUST survive the empty-partition early return")
	assert.Equal(t, "after", written.Description)

	got := profileUpdateEnvelope(t, outbox)
	assert.Equal(t, []string{"description"}, got.ChangedAttributes)
	assert.NotContains(t, string(outbox.rows[0].Payload), "after")
	assert.NotContains(t, string(outbox.rows[0].Payload), "before")
}

// TestUpdateCharacterProfileAttributesDescriptionEqualToStoredIsANoOp pins the
// fourth of the four placement rules: option supplied but EQUAL to the stored
// value is the documented no-op, same as any other unchanged field.
func TestUpdateCharacterProfileAttributesDescriptionEqualToStoredIsANoOp(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, _, outbox := profileTxFixture(t, subjectID, charID)
	stored := &world.Character{ID: charID, Name: "Alice", Version: 3, Status: world.StatusActive, Description: "unchanged"}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	// No Update expectation: reaching the writer at all fails the mock.

	err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3,
		map[string]string{}, world.WithDescription("unchanged"))
	require.NoError(t, err, "a supplied-but-equal description is a no-op success")
	assert.Empty(t, outbox.rows, "a no-op emits no envelope")
}

// TestUpdateCharacterProfileAttributesSkipUnchangedPropertiesSuppressesTheEqualValuedRewrite
// is the C3-26 case, and it is the one the admin no-empty-envelope guarantee
// actually turns on.
//
// The shipped method rewrites an equal-valued row UNCONDITIONALLY: the row lands
// in `updates`, so the write, the CAS and the version bump all happen while
// `changed` stays empty. A handler precheck could not suppress that even absent
// the race — and a pre-transaction comparison reads rows it holds no lock on, so
// a concurrent write between the comparison and the transaction makes it wrong
// in both directions. The option moves the decision INSIDE the transaction,
// where the rows are already read and the CAS already protects them.
func TestUpdateCharacterProfileAttributesSkipUnchangedPropertiesSuppressesTheEqualValuedRewrite(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
	seedProfileRow(props, charID, "profile.biography", "identical prose")

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	// No Update expectation: a true no-op never reaches the character writer, so
	// no row is rewritten and no version is bumped.

	err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
		map[string]string{"profile.biography": "identical prose"},
		world.WithSkipUnchangedProperties())
	require.NoError(t, err)
	assert.Empty(t, outbox.rows,
		"a non-empty mask naming only equal-valued fields emits NO envelope under the admin option")
}

// TestUpdateCharacterProfileAttributesSkipUnchangedPropertiesStillWritesARealChange
// is the paired positive control: the option suppresses the UNCHANGED names and
// nothing else. Without it, the test above could pass under an option that
// suppressed every write.
func TestUpdateCharacterProfileAttributesSkipUnchangedPropertiesStillWritesARealChange(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
	seedProfileRow(props, charID, "profile.biography", "identical prose")
	seedProfileRow(props, charID, "profile.concept", "an archivist")

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
		Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}, nil)

	err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
		map[string]string{
			"profile.biography": "identical prose",     // unchanged -> dropped
			"profile.concept":   "a retired archivist", // changed  -> written
		},
		world.WithSkipUnchangedProperties())
	require.NoError(t, err)

	row, ok := props.lookup("character", charID, "profile.concept")
	require.True(t, ok)
	require.NotNil(t, row.Value)
	assert.Equal(t, "a retired archivist", *row.Value)

	got := profileUpdateEnvelope(t, outbox)
	assert.Equal(t, []string{"profile.concept"}, got.ChangedAttributes)
}

// TestUpdateCharacterProfileAttributesWithNoOptionsKeepsTheShippedPlayerContract
// is what stops the admin fix from silently changing a live player contract.
//
// The PLAYER facade supplies no options. Its all-identical resubmit MUST still
// rewrite the row, bump the version and emit ONE envelope with an EMPTY
// changed_attributes — the "both representable and honest" contract world.Service
// documents. Making the skip unconditional instead of option-gated fails here.
func TestUpdateCharacterProfileAttributesWithNoOptionsKeepsTheShippedPlayerContract(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
	seedProfileRow(props, charID, "profile.biography", "identical prose")

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	var written *world.Character
	mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
		Run(func(_ context.Context, c *world.Character) { written = c }).
		Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
			Type: wmodel.AggregateCharacter, ID: charID, BeforeVersion: 5, AfterVersion: 6,
		}}, nil)

	// NO OPTIONS — the player path, verbatim.
	err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
		map[string]string{"profile.biography": "identical prose"})
	require.NoError(t, err)

	require.NotNil(t, written, "the row IS rewritten and the version IS bumped: unconditional, by contract")
	got := profileUpdateEnvelope(t, outbox)
	assert.Empty(t, got.ChangedAttributes,
		"ONE envelope with an EMPTY changed list — representable and honest")
	assert.Empty(t, got.Section, "the player path supplies no admin context")
	assert.Empty(t, got.Action)
}
