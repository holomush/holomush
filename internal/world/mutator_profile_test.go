// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// The executor-level half of the character profile-attribute write (04-09
// Task 1). These specs drive world.Service.UpdateCharacterProfileAttributes over
// a TRANSACTIONAL fake stack — a rollback-capable transactor over an in-memory
// property store and an append-only outbox — because the contract under test is
// precisely the one a passthrough transactor cannot express: the attribute rows
// and the single world-change envelope commit or roll back TOGETHER.
//
// worldMutator and its closure builders are unexported, and package world's own
// test binary cannot import internal/world/worldtest (import cycle — worldtest
// imports world, see export_test.go's note). So the executor seam is driven
// through its only production entry point, the exported Service command.

// fakeProfilePropertyStore is an in-memory world.PropertyRepository whose
// contents can be snapshotted and restored, so profileRollbackTransactor can
// model a real ROLLBACK. failOnName arms a forced failure at a chosen attribute
// so a test can prove that a row written EARLIER in the same closure does not
// survive.
type fakeProfilePropertyStore struct {
	rows       map[ulid.ULID]world.EntityProperty
	failOnName string
	failErr    error
}

func newFakeProfilePropertyStore(seed ...world.EntityProperty) *fakeProfilePropertyStore {
	s := &fakeProfilePropertyStore{rows: make(map[ulid.ULID]world.EntityProperty, len(seed))}
	for _, p := range seed {
		s.rows[p.ID] = p
	}
	return s
}

func (s *fakeProfilePropertyStore) snapshot() map[ulid.ULID]world.EntityProperty {
	out := make(map[ulid.ULID]world.EntityProperty, len(s.rows))
	for k, v := range s.rows {
		out[k] = v
	}
	return out
}

func (s *fakeProfilePropertyStore) restore(snap map[ulid.ULID]world.EntityProperty) {
	s.rows = snap
}

// lookup finds a row by its parent-scoped unique key, mirroring the
// entity_properties_parent_name_unique constraint.
func (s *fakeProfilePropertyStore) lookup(parentType string, parentID ulid.ULID, name string) (world.EntityProperty, bool) {
	for _, p := range s.rows {
		if p.ParentType == parentType && p.ParentID == parentID && p.Name == name {
			return p, true
		}
	}
	return world.EntityProperty{}, false
}

func (s *fakeProfilePropertyStore) Get(_ context.Context, id ulid.ULID) (*world.EntityProperty, error) {
	p, ok := s.rows[id]
	if !ok {
		return nil, oops.Code("PROPERTY_NOT_FOUND").Wrapf(world.ErrNotFound, "get property %s", id)
	}
	out := p
	return &out, nil
}

func (s *fakeProfilePropertyStore) ListByParent(_ context.Context, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error) {
	out := make([]*world.EntityProperty, 0, len(s.rows))
	for _, p := range s.rows {
		if p.ParentType == parentType && p.ParentID == parentID {
			row := p
			out = append(out, &row)
		}
	}
	slices.SortFunc(out, func(a, b *world.EntityProperty) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *fakeProfilePropertyStore) Create(_ context.Context, p *world.EntityProperty) error {
	if s.failOnName != "" && p.Name == s.failOnName {
		return s.failErr
	}
	if _, dup := s.lookup(p.ParentType, p.ParentID, p.Name); dup {
		return oops.Code("PROPERTY_CREATE_FAILED").
			Errorf("entity_properties_parent_name_unique violated for %s/%s", p.ParentType, p.Name)
	}
	s.rows[p.ID] = *p
	return nil
}

func (s *fakeProfilePropertyStore) Update(_ context.Context, p *world.EntityProperty) error {
	if s.failOnName != "" && p.Name == s.failOnName {
		return s.failErr
	}
	if _, ok := s.rows[p.ID]; !ok {
		return oops.Code("PROPERTY_NOT_FOUND").Wrapf(world.ErrNotFound, "update property %s", p.ID)
	}
	s.rows[p.ID] = *p
	return nil
}

func (s *fakeProfilePropertyStore) Delete(_ context.Context, id ulid.ULID) error {
	p, ok := s.rows[id]
	if !ok {
		return oops.Code("PROPERTY_NOT_FOUND").Wrapf(world.ErrNotFound, "delete property %s", id)
	}
	if s.failOnName != "" && p.Name == s.failOnName {
		return s.failErr
	}
	delete(s.rows, id)
	return nil
}

func (s *fakeProfilePropertyStore) DeleteByParent(_ context.Context, parentType string, parentID ulid.ULID) error {
	for id, p := range s.rows {
		if p.ParentType == parentType && p.ParentID == parentID {
			delete(s.rows, id)
		}
	}
	return nil
}

// profileTxOutbox is an append-only world.OutboxWriter whose committed rows can
// be truncated by the rollback transactor, so "no envelope survives a rolled-back
// write" is an assertion about state rather than about a call counter.
type profileTxOutbox struct {
	rows      []wmodel.EnvelopeIntent
	lastDelta *wmodel.MutationDelta
}

func (o *profileTxOutbox) WriteIntent(_ context.Context, intent wmodel.EnvelopeIntent, delta *wmodel.MutationDelta) (*wmodel.Envelope, error) {
	o.rows = append(o.rows, intent)
	o.lastDelta = delta
	return wmodel.Finalize(intent, delta, 1, int64(len(o.rows))), nil
}

// profileRollbackTransactor snapshots the property store and the outbox before
// running the closure and RESTORES both when it returns an error — the in-memory
// stand-in for a real ROLLBACK.
type profileRollbackTransactor struct {
	props  *fakeProfilePropertyStore
	outbox *profileTxOutbox
}

func (tr *profileRollbackTransactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	propSnapshot := tr.props.snapshot()
	outboxSnapshot := len(tr.outbox.rows)
	if err := fn(ctx); err != nil {
		tr.props.restore(propSnapshot)
		tr.outbox.rows = tr.outbox.rows[:outboxSnapshot]
		return err
	}
	return nil
}

// profileTxFixture wires a Service over a mocked character repository plus the
// transactional fake stack, granting the given ABAC action on the character.
func profileTxFixture(
	t *testing.T,
	subjectID, action string,
	charID ulid.ULID,
	seed ...world.EntityProperty,
) (*world.Service, *worldtest.MockCharacterRepository, *fakeProfilePropertyStore, *profileTxOutbox) {
	t.Helper()
	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockCharacterRepository(t)
	props := newFakeProfilePropertyStore(seed...)
	outbox := &profileTxOutbox{}
	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockRepo,
		PropertyRepo:  props,
		Engine:        engine,
		Transactor:    &profileRollbackTransactor{props: props, outbox: outbox},
		OutboxWriter:  outbox,
	})
	engine.Grant(subjectID, action, access.CharacterResource(charID.String()))
	return svc, mockRepo, props, outbox
}

func TestWorldServiceUpdateCharacterProfileAttributesExecutorSeam(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("commits the attribute row and exactly one envelope from the guarded character update's delta", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, "write", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 3, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
			Type:          wmodel.AggregateCharacter,
			ID:            charID,
			BeforeVersion: 3,
			AfterVersion:  4,
		}}
		mockRepo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(c *world.Character) bool {
			return c.ID == charID && c.Version == 3
		})).Return(delta, nil)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3,
			map[string]string{"profile.pronouns": "she/her"})
		require.NoError(t, err)

		row, ok := props.lookup("character", charID, "profile.pronouns")
		require.True(t, ok, "the attribute row committed")
		require.NotNil(t, row.Value)
		assert.Equal(t, "she/her", *row.Value)

		require.Len(t, outbox.rows, 1, "a profile write emits exactly one envelope")
		assert.Equal(t, "character_profile_update", outbox.rows[0].Kind)
		assert.Equal(t, charID, outbox.rows[0].AggregateID)
		// The envelope's manifest is finalized from the REAL repo delta the
		// guarded character update returned, never a hand-constructed one.
		assert.Same(t, delta, outbox.lastDelta)
	})

	t.Run("rolls back both the attribute row and the envelope when a write inside the closure fails", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, "write", charID)
		// Fail on the SECOND attribute (names are applied in sorted order), so the
		// assertion is that the first attribute's committed row does not survive —
		// not merely that a failing write wrote nothing.
		props.failOnName = "profile.concept"
		props.failErr = oops.Code("PROPERTY_CREATE_FAILED").Errorf("forced failure")

		stored := &world.Character{ID: charID, Name: "Alice", Version: 3, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().Update(mock.Anything, mock.Anything).
			Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}, nil)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 3, map[string]string{
			"profile.age":     "ageless",
			"profile.concept": "the one that fails",
		})
		require.Error(t, err)

		_, ageOK := props.lookup("character", charID, "profile.age")
		assert.False(t, ageOK, "the earlier attribute row is rolled back with the failing one")
		_, conceptOK := props.lookup("character", charID, "profile.concept")
		assert.False(t, conceptOK)
		assert.Empty(t, outbox.rows, "no envelope survives a rolled-back write")
	})

	t.Run("aborts on a stale expected version with the concurrent-edit signal and writes no attribute row", func(t *testing.T) {
		svc, mockRepo, props, outbox := profileTxFixture(t, subjectID, "write", charID)

		// The stored version is 9 and the CALLER's expected version is 4. The mock
		// is armed ONLY for a CAS carrying 4, so an implementation that guarded on
		// the freshly-read char.Version would fail here as an unexpected call —
		// this is what makes the caller-version guarantee (INV-WORLD-7) non-vacuous.
		stored := &world.Character{ID: charID, Name: "Alice", Version: 9, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockRepo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(c *world.Character) bool {
			return c.Version == 4
		})).Return(nil, conflict)

		err := svc.UpdateCharacterProfileAttributes(ctx, world.HumanCaller(subjectID), charID, 4,
			map[string]string{"profile.pronouns": "they/them"})
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		_, ok := props.lookup("character", charID, "profile.pronouns")
		assert.False(t, ok, "the version-guarded character update fails FIRST, before any property work")
		assert.Empty(t, outbox.rows)
	})
}
