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
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, "write", charID)
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
