// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeVersionQuerier returns fixed entities carrying a read-time Version so the
// RMW mutator's version threading can be observed.
type fakeVersionQuerier struct {
	loc *world.Location
	obj *world.Object
}

func (f *fakeVersionQuerier) GetLocation(_ context.Context, _ ulid.ULID) (*world.Location, error) {
	return f.loc, nil
}

func (f *fakeVersionQuerier) GetObject(_ context.Context, _ ulid.ULID) (*world.Object, error) {
	return f.obj, nil
}

// fakeVersionMutator records the version each guarded write received and can
// simulate the guarded repo's WORLD_CONCURRENT_EDIT on a stale write.
type fakeVersionMutator struct {
	gotLocationVersion int
	gotObjectVersion   int
	locationUpdates    int
	objectUpdates      int
	conflict           bool
}

func (f *fakeVersionMutator) UpdateLocation(_ context.Context, _ string, loc *world.Location) error {
	f.gotLocationVersion = loc.Version
	f.locationUpdates++
	if f.conflict {
		return oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	}
	return nil
}

func (f *fakeVersionMutator) UpdateObject(_ context.Context, _ string, obj *world.Object) error {
	f.gotObjectVersion = obj.Version
	f.objectUpdates++
	if f.conflict {
		return oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	}
	return nil
}

// TestEntityMutator_PropertyWriteFunnelsToExactlyOneParentUpdate proves a
// property write (SetName/SetDescription) results in EXACTLY ONE parent-aggregate
// update — the parent command's envelope-emitting UpdateLocation/UpdateObject
// (05-10) — and never a second, property-level write that would double-emit. The
// property write's envelope IS the parent's update envelope (05-11 property-leak
// resolution): entity_properties is written on the parent's execerFromCtx (05-14)
// inside that one mutation tx, so the parent command emits one envelope, not two.
func TestEntityMutator_PropertyWriteFunnelsToExactlyOneParentUpdate(t *testing.T) {
	t.Run("location SetName -> exactly one UpdateLocation", func(t *testing.T) {
		q := &fakeVersionQuerier{loc: &world.Location{Name: "Old", Version: 7}}
		m := &fakeVersionMutator{}
		require.NoError(t, locationEntityMutator{}.SetName(context.Background(), q, m, "subject", ulid.Make(), "New"))
		assert.Equal(t, 1, m.locationUpdates, "one property write funnels to exactly one parent update (no duplicate envelope)")
		assert.Equal(t, 0, m.objectUpdates)
	})
	t.Run("object SetDescription -> exactly one UpdateObject", func(t *testing.T) {
		q := &fakeVersionQuerier{obj: &world.Object{Description: "Old", Version: 4}}
		m := &fakeVersionMutator{}
		require.NoError(t, objectEntityMutator{}.SetDescription(context.Background(), q, m, "subject", ulid.Make(), "New"))
		assert.Equal(t, 1, m.objectUpdates, "one property write funnels to exactly one parent update (no duplicate envelope)")
		assert.Equal(t, 0, m.locationUpdates)
	})
}

func TestLocationEntityMutator_SetName_ThreadsReadVersion(t *testing.T) {
	q := &fakeVersionQuerier{loc: &world.Location{Name: "Old", Version: 7}}
	m := &fakeVersionMutator{}

	err := locationEntityMutator{}.SetName(context.Background(), q, m, "subject", ulid.Make(), "New")
	require.NoError(t, err)
	// The write MUST carry the version read at the start of the RMW, not 0.
	assert.Equal(t, 7, m.gotLocationVersion)
}

func TestLocationEntityMutator_SetDescription_SurfacesConcurrentEdit(t *testing.T) {
	q := &fakeVersionQuerier{loc: &world.Location{Description: "Old", Version: 7}}
	m := &fakeVersionMutator{conflict: true}

	err := locationEntityMutator{}.SetDescription(context.Background(), q, m, "subject", ulid.Make(), "New")
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrConcurrentEdit)
	errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
}

func TestObjectEntityMutator_SetName_ThreadsReadVersion(t *testing.T) {
	q := &fakeVersionQuerier{obj: &world.Object{Name: "Old", Version: 4}}
	m := &fakeVersionMutator{}

	err := objectEntityMutator{}.SetName(context.Background(), q, m, "subject", ulid.Make(), "New")
	require.NoError(t, err)
	assert.Equal(t, 4, m.gotObjectVersion)
}

func TestObjectEntityMutator_SetDescription_SurfacesConcurrentEdit(t *testing.T) {
	q := &fakeVersionQuerier{obj: &world.Object{Description: "Old", Version: 4}}
	m := &fakeVersionMutator{conflict: true}

	err := objectEntityMutator{}.SetDescription(context.Background(), q, m, "subject", ulid.Make(), "New")
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrConcurrentEdit)
	errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
}

type testEntityMutator struct {
	entityType string
}

func (t testEntityMutator) EntityType() string {
	return t.entityType
}

func (t testEntityMutator) GetName(_ context.Context, _ WorldQuerier, _ ulid.ULID) (string, error) {
	return "", nil
}

func (t testEntityMutator) SetName(_ context.Context, _ WorldQuerier, _ WorldMutator, _ string, _ ulid.ULID, _ string) error {
	return nil
}

func (t testEntityMutator) GetDescription(_ context.Context, _ WorldQuerier, _ ulid.ULID) (string, error) {
	return "", nil
}

func (t testEntityMutator) SetDescription(_ context.Context, _ WorldQuerier, _ WorldMutator, _ string, _ ulid.ULID, _ string) error {
	return nil
}

func TestEntityMutatorRegistry_RegisterAndLookup(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	mutator := testEntityMutator{entityType: "location"}
	require.NoError(t, registry.Register(mutator))

	got, ok := registry.Lookup("location")
	require.True(t, ok)
	assert.Equal(t, mutator, got)
}

func TestEntityMutatorRegistry_Register_Duplicate(t *testing.T) {
	registry := NewEntityMutatorRegistry()
	mutator := testEntityMutator{entityType: "location"}

	require.NoError(t, registry.Register(mutator))
	err := registry.Register(mutator)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateEntityMutator)
}

func TestEntityMutatorRegistry_Register_NilMutator(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	err := registry.Register(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator cannot be nil")
}

func TestEntityMutatorRegistry_Register_EmptyEntityType(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	err := registry.Register(testEntityMutator{entityType: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntityType)
}

func TestEntityMutatorRegistry_DefaultRegistrations(t *testing.T) {
	registry := DefaultEntityMutatorRegistry()

	_, ok := registry.Lookup("location")
	assert.True(t, ok)

	_, ok = registry.Lookup("object")
	assert.True(t, ok)
}

func TestEntityMutatorRegistry_RegisteredTypes_Sorted(t *testing.T) {
	registry := NewEntityMutatorRegistry()
	require.NoError(t, registry.Register(testEntityMutator{entityType: "widget"}))
	require.NoError(t, registry.Register(testEntityMutator{entityType: "location"}))

	assert.Equal(t, []string{"location", "widget"}, registry.RegisteredTypes())
}
