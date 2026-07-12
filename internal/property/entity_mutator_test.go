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
	conflict           bool
}

func (f *fakeVersionMutator) UpdateLocation(_ context.Context, _ string, loc *world.Location) error {
	f.gotLocationVersion = loc.Version
	if f.conflict {
		return oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	}
	return nil
}

func (f *fakeVersionMutator) UpdateObject(_ context.Context, _ string, obj *world.Object) error {
	f.gotObjectVersion = obj.Version
	if f.conflict {
		return oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
	}
	return nil
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
