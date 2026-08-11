// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// The command-level half of the character profile-attribute write (04-09).
// These specs drive world.Service.UpdateCharacterProfileAttributes end to end —
// guard chain, ABAC decision on character:<id>, the create/update/delete
// partition, and the mutate() seam — over the transactional fake stack built in
// mutator_profile_test.go.

func TestWorldServiceUpdateCharacterProfileAttributesFirstWrite(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	t.Run("writes the first profile attribute for a character with no property rows at all", func(t *testing.T) {
		// The fixture seeds ZERO entity_properties rows. This is the load-bearing
		// case: a gate on the PROPERTY resource could never authorize it, because
		// resolving resource.property.owner requires fetching a row that does not
		// exist yet and PropertyProvider.ResolveResource fails closed. The gate is
		// ABAC `write` on character:<id> (01-SPEC §9.3).
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
		require.Empty(t, props.rows, "the fixture starts with no property rows")

		stored := &world.Character{ID: charID, Name: "Alice", Version: 2, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(c *world.Character) bool {
			return c.ID == charID && c.Version == 2
		})).Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
			Type:          wmodel.AggregateCharacter,
			ID:            charID,
			BeforeVersion: 2,
			AfterVersion:  3,
		}}, nil)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 2,
			map[string]string{"profile.biography": "Born under a red sky."})
		require.NoError(t, err)

		// Every field of the created row is asserted individually. Asserting only
		// that a row exists would pass against every construction defect this
		// contract exists to prevent — a nil Owner (which makes every later per-row
		// property decision wrong and is unrecoverable without a backfill) and an
		// empty Visibility (which fails the CHECK constraint, because Create passes
		// p.Visibility straight into the INSERT and the column DEFAULT never applies).
		row, ok := props.lookup("character", charID, "profile.biography")
		require.True(t, ok, "the attribute row was created")
		assert.Equal(t, "character", row.ParentType)
		assert.Equal(t, charID, row.ParentID)
		require.NotNil(t, row.Value)
		assert.Equal(t, "Born under a red sky.", *row.Value)
		require.NotNil(t, row.Owner, "Owner is set so seed:property-owner-write and seed:property-private-read can key on it")
		assert.Equal(t, charID.String(), *row.Owner)
		assert.Equal(t, "public", row.Visibility,
			"term B on the viewer path is seed:viewer-property-public-read; the withholding lever is the per-attribute tier floor, not this column")
		assert.NotEqual(t, ulid.ULID{}, row.ID, "the row carries an idgen.New() primary key")
		assert.False(t, row.CreatedAt.IsZero())

		require.Len(t, outbox.rows, 1, "exactly one envelope per successful profile write")
		assert.Equal(t, "character_profile_update", outbox.rows[0].Kind)
		assert.Equal(t, wmodel.AggregateCharacter, outbox.rows[0].AggregateType)
		assert.Equal(t, charID, outbox.rows[0].AggregateID)
		assert.Equal(t, subjectID, outbox.rows[0].Actor)
	})
}

// seedProfileRow inserts a pre-existing profile row directly into the fake store,
// standing in for a row an earlier write committed.
func seedProfileRow(props *fakeProfilePropertyStore, charID ulid.ULID, name, value string) world.EntityProperty {
	owner := charID.String()
	stored := value
	row := world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "character",
		ParentID:   charID,
		Name:       name,
		Value:      &stored,
		Owner:      &owner,
		Visibility: "public",
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
	}
	props.rows[row.ID] = row
	return row
}

func TestWorldServiceUpdateCharacterProfileAttributesPartition(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	t.Run("updates the existing row in place rather than creating a second one", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
		existing := seedProfileRow(props, charID, "profile.concept", "a wandering archivist")

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
			Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}, nil)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
			map[string]string{"profile.concept": "a retired archivist"})
		require.NoError(t, err)

		// One row, not two: the fake store enforces the
		// entity_properties_parent_name_unique key, so a create on an existing
		// name would have errored rather than silently duplicating.
		require.Len(t, props.rows, 1, "the write updates the row in place")
		row, ok := props.lookup("character", charID, "profile.concept")
		require.True(t, ok)
		require.NotNil(t, row.Value)
		assert.Equal(t, "a retired archivist", *row.Value)

		// An update must not re-home ownership, re-open visibility, or mint a new
		// identity for the row — that is where a careless "rebuild the struct and
		// call Update" implementation silently loses the per-row ABAC anchors.
		assert.Equal(t, existing.ID, row.ID, "the row keeps its identity")
		require.NotNil(t, row.Owner)
		assert.Equal(t, *existing.Owner, *row.Owner, "the row keeps its owner")
		assert.Equal(t, existing.Visibility, row.Visibility, "the row keeps its visibility")
		assert.Equal(t, existing.CreatedAt, row.CreatedAt)

		require.Len(t, outbox.rows, 1)
	})

	t.Run("removes the row when the new value is the empty string", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)
		seedProfileRow(props, charID, "profile.currently", "haunting the stacks")

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
			Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}, nil)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
			map[string]string{"profile.currently": ""})
		require.NoError(t, err)

		// ABSENT, not present-with-an-empty-value: a cleared field must not remain
		// a row whose value happens to be "".
		_, ok := props.lookup("character", charID, "profile.currently")
		assert.False(t, ok, "clearing a field removes its row")
		assert.Empty(t, props.rows)
		require.Len(t, outbox.rows, 1, "a clear is a real change and carries its envelope")
	})
}

func TestWorldServiceUpdateCharacterProfileAttributesGuards(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	t.Run("rejects an attribute name outside the twelve before any read or write", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
			map[string]string{"profile.nickname": "Al"})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN")
		// The closed set is enforced by the DOMAIN method, with no facade
		// involved — the facade allowlist is defense in depth, not the only gate.
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "Update")
		assert.Empty(t, props.rows)
		assert.Empty(t, outbox.rows)
	})

	t.Run("rejects the settings column's name, which is not a profile attribute", func(t *testing.T) {
		// 01-SPEC §7.2 calls this collision out deliberately: profile.rp_preferences
		// is published profile prose, characters.preferences is the settings column.
		// A bare "preferences" key belongs to neither and must not be writable here.
		svc, mockRepo, _, outbox := profileTxFixture(t, subjectID, charID)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 5,
			map[string]string{"preferences": "{}"})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN")
		mockRepo.AssertNotCalled(t, "Get")
		assert.Empty(t, outbox.rows)
	})

	for _, version := range []int{0, -1} {
		t.Run("rejects a non-positive expected version before any read", func(t *testing.T) {
			svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)

			err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, version,
				map[string]string{"profile.pronouns": "she/her"})
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "CHARACTER_VERSION_REQUIRED")
			// Asserting the repository was NEVER called is what proves the guard
			// genuinely PRECEDES the read, rather than merely returning the right
			// code after it.
			mockRepo.AssertNotCalled(t, "Get")
			mockRepo.AssertNotCalled(t, "Update")
			assert.Empty(t, props.rows)
			assert.Empty(t, outbox.rows)
		})
	}

	t.Run("surfaces a stale expected version as a concurrent edit, distinct from a not-found", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 8, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
			Return(nil, oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit))

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3,
			map[string]string{"profile.pronouns": "she/her"})
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		assert.NotErrorIs(t, err, world.ErrNotFound,
			"a conflict is deliberately NOT a not-found: collapsing them would tell a stale caller their view was authoritative")
		assert.Empty(t, props.rows)
		assert.Empty(t, outbox.rows)
	})

	t.Run("surfaces a missing character as a not-found, distinct from a concurrent edit", func(t *testing.T) {
		// The other half of the distinctness: without it, a test suite could not
		// tell an error mapping that collapses both signals into one code.
		svc, mockRepo, _, outbox := profileTxFixture(t, subjectID, charID)

		mockRepo.EXPECT().Get(ctx, charID).
			Return(nil, oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound))

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3,
			map[string]string{"profile.pronouns": "she/her"})
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
		assert.NotErrorIs(t, err, world.ErrConcurrentEdit)
		assert.Empty(t, outbox.rows)
	})
}

// profileSeedFixture wires a Service whose ABAC decisions come from a REAL
// policy.Engine over the WHOLE SHIPPED seed corpus (policy.SeedPolicies), so the
// permit under test is seed:player-self-access itself rather than a hand-built
// grant that could drift from the text that ships.
func profileSeedFixture(
	t *testing.T,
	charRepo world.CharacterRepository,
) (*world.Service, *fakeProfilePropertyStore, *profileTxOutbox) {
	t.Helper()
	engine := abactest.NewSeedEngine(t, attribute.NewCharacterProvider(charRepo, nil))
	props := newFakeProfilePropertyStore()
	outbox := &profileTxOutbox{}
	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: charRepo,
		PropertyRepo:  props,
		Engine:        engine,
		Transactor:    &profileRollbackTransactor{props: props, outbox: outbox},
		OutboxWriter:  outbox,
	})
	return svc, props, outbox
}

func TestWorldServiceUpdateCharacterProfileAttributesSeedAuthorization(t *testing.T) {
	ctx := context.Background()

	t.Run("denies a stranger and permits the owner on the same fixture", func(t *testing.T) {
		ownerID := ulid.Make()
		strangerID := ulid.Make()
		repo := worldtest.NewMockCharacterRepository(t)
		owner := &world.Character{ID: ownerID, Name: "Alice", Version: 1, Status: world.StatusActive}
		stranger := &world.Character{ID: strangerID, Name: "Mallory", Version: 1, Status: world.StatusActive}
		repo.EXPECT().Get(mock.Anything, ownerID).Return(owner, nil)
		repo.EXPECT().Get(mock.Anything, strangerID).Return(stranger, nil)
		svc, props, outbox := profileSeedFixture(t, repo)

		// DENY. seed:player-self-access permits `write` on a character resource
		// only when resource.character.id == principal.character.id.
		err := svc.UpdateCharacterProfileAttributes(ctx,
			world.HumanCaller(access.CharacterSubject(strangerID.String())), ownerID, 1,
			map[string]string{"profile.concept": "hijacked"})
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		assert.Empty(t, props.rows, "a denied write changes no row")
		assert.Empty(t, outbox.rows, "a denied write emits no envelope")

		// PERMIT, the PAIRED positive control on the SAME fixture. Without it a
		// blanket-deny engine would make the deny half pass vacuously.
		repo.EXPECT().Update(mock.Anything, mock.Anything).
			Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: ownerID}}, nil)

		err = svc.UpdateCharacterProfileAttributes(ctx,
			world.HumanCaller(access.CharacterSubject(ownerID.String())), ownerID, 1,
			map[string]string{"profile.concept": "an owner-authored concept"})
		require.NoError(t, err)
		row, ok := props.lookup("character", ownerID, "profile.concept")
		require.True(t, ok, "the owner's write commits")
		require.NotNil(t, row.Value)
		assert.Equal(t, "an owner-authored concept", *row.Value)
		require.Len(t, outbox.rows, 1)
		assert.Equal(t, "character_profile_update", outbox.rows[0].Kind)
	})
}
