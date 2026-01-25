// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// mockWorldService implements hostfunc.WorldService for testing.
type mockWorldService struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
	object     *world.Object
	err        error

	// Capture the subject ID passed to each method
	capturedSubjectID string
}

func (m *mockWorldService) GetLocation(_ context.Context, subjectID string, _ ulid.ULID) (*world.Location, error) {
	m.capturedSubjectID = subjectID
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldService) GetCharacter(_ context.Context, subjectID string, _ ulid.ULID) (*world.Character, error) {
	m.capturedSubjectID = subjectID
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldService) GetCharactersByLocation(_ context.Context, subjectID string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	m.capturedSubjectID = subjectID
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

func (m *mockWorldService) GetObject(_ context.Context, subjectID string, _ ulid.ULID) (*world.Object, error) {
	m.capturedSubjectID = subjectID
	if m.err != nil {
		return nil, m.err
	}
	return m.object, nil
}

// Compile-time interface check.
var _ hostfunc.WorldService = (*mockWorldService)(nil)

func TestNewWorldQuerierAdapter_Validation(t *testing.T) {
	t.Run("panics when service is nil", func(t *testing.T) {
		assert.PanicsWithValue(t, "hostfunc.NewWorldQuerierAdapter: service is required", func() {
			hostfunc.NewWorldQuerierAdapter(nil, "my-plugin")
		})
	})

	t.Run("panics when pluginName is empty", func(t *testing.T) {
		svc := &mockWorldService{}
		assert.PanicsWithValue(t, "hostfunc.NewWorldQuerierAdapter: pluginName is required", func() {
			hostfunc.NewWorldQuerierAdapter(svc, "")
		})
	})

	t.Run("succeeds with valid inputs", func(t *testing.T) {
		svc := &mockWorldService{}
		assert.NotPanics(t, func() {
			adapter := hostfunc.NewWorldQuerierAdapter(svc, "my-plugin")
			assert.NotNil(t, adapter)
		})
	})
}

func TestWorldQuerierAdapter_SubjectID(t *testing.T) {
	svc := &mockWorldService{}
	adapter := hostfunc.NewWorldQuerierAdapter(svc, "my-plugin")

	assert.Equal(t, "system:plugin:my-plugin", adapter.SubjectID())
}

func TestWorldQuerierAdapter_GetLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()

	t.Run("returns location and passes correct subject ID", func(t *testing.T) {
		expectedLoc := &world.Location{
			ID:   locID,
			Name: "Test Room",
		}
		svc := &mockWorldService{location: expectedLoc}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		loc, err := adapter.GetLocation(ctx, locID)

		require.NoError(t, err)
		assert.Equal(t, expectedLoc, loc)
		assert.Equal(t, "system:plugin:test-plugin", svc.capturedSubjectID)
	})

	t.Run("propagates errors", func(t *testing.T) {
		expectedErr := errors.New("authorization denied")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		loc, err := adapter.GetLocation(ctx, locID)

		assert.Nil(t, loc)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error includes code and context", func(t *testing.T) {
		expectedErr := errors.New("underlying error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "my-plugin")

		_, err := adapter.GetLocation(ctx, locID)

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
		errutil.AssertErrorContext(t, err, "plugin", "my-plugin")
		errutil.AssertErrorContext(t, err, "entity_type", "location")
	})

	// Defensive nil check: If service returns (nil, nil), treat as ErrNotFound.
	// This is a bug in the underlying service, but we handle it gracefully
	// to prevent nil pointer panics in plugins.
	t.Run("handles nil location without error", func(t *testing.T) {
		svc := &mockWorldService{location: nil, err: nil}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		loc, err := adapter.GetLocation(ctx, locID)

		require.Error(t, err)
		assert.Nil(t, loc)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})
}

func TestWorldQuerierAdapter_GetCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()

	t.Run("returns character and passes correct subject ID", func(t *testing.T) {
		locID := ulid.Make()
		expectedChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &locID,
		}
		svc := &mockWorldService{character: expectedChar}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "chat-plugin")

		char, err := adapter.GetCharacter(ctx, charID)

		require.NoError(t, err)
		assert.Equal(t, expectedChar, char)
		assert.Equal(t, "system:plugin:chat-plugin", svc.capturedSubjectID)
	})

	t.Run("propagates errors", func(t *testing.T) {
		expectedErr := world.ErrNotFound
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		char, err := adapter.GetCharacter(ctx, charID)

		assert.Nil(t, char)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error includes code and context", func(t *testing.T) {
		expectedErr := errors.New("underlying error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "char-plugin")

		_, err := adapter.GetCharacter(ctx, charID)

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
		errutil.AssertErrorContext(t, err, "plugin", "char-plugin")
		errutil.AssertErrorContext(t, err, "entity_type", "character")
	})

	// Defensive nil check: If service returns (nil, nil), treat as ErrNotFound.
	// This is a bug in the underlying service, but we handle it gracefully
	// to prevent nil pointer panics in plugins.
	t.Run("handles nil character without error", func(t *testing.T) {
		svc := &mockWorldService{character: nil, err: nil}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		char, err := adapter.GetCharacter(ctx, charID)

		require.Error(t, err)
		assert.Nil(t, char)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})
}

func TestWorldQuerierAdapter_GetCharactersByLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()

	t.Run("returns characters and passes correct subject ID", func(t *testing.T) {
		char1 := &world.Character{ID: ulid.Make(), Name: "Char1", LocationID: &locID}
		char2 := &world.Character{ID: ulid.Make(), Name: "Char2", LocationID: &locID}
		expectedChars := []*world.Character{char1, char2}

		svc := &mockWorldService{characters: expectedChars}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "presence-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		require.NoError(t, err)
		assert.Equal(t, expectedChars, chars)
		assert.Equal(t, "system:plugin:presence-plugin", svc.capturedSubjectID)
	})

	t.Run("returns empty slice", func(t *testing.T) {
		svc := &mockWorldService{characters: []*world.Character{}}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		require.NoError(t, err)
		assert.Empty(t, chars)
	})

	// Defensive nil check: If service returns nil slice, normalize to empty slice.
	// Unlike single-entity methods, nil slices are technically valid,
	// so we normalize rather than return an error.
	t.Run("normalizes nil slice to empty slice", func(t *testing.T) {
		svc := &mockWorldService{characters: nil, err: nil}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		require.NoError(t, err)
		assert.NotNil(t, chars, "nil slice should be normalized to empty slice")
		assert.Empty(t, chars)
	})

	t.Run("propagates errors", func(t *testing.T) {
		expectedErr := errors.New("database error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		assert.Nil(t, chars)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error includes code and context", func(t *testing.T) {
		expectedErr := errors.New("underlying error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "loc-plugin")

		_, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
		errutil.AssertErrorContext(t, err, "plugin", "loc-plugin")
		errutil.AssertErrorContext(t, err, "entity_type", "characters_by_location")
	})
}

func TestWorldQuerierAdapter_GetObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()

	t.Run("returns object and passes correct subject ID", func(t *testing.T) {
		locID := ulid.Make()
		expectedObj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
		require.NoError(t, err)
		expectedObj.Description = "A glowing blade of ancient power."
		svc := &mockWorldService{object: expectedObj}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "inventory-plugin")

		obj, err := adapter.GetObject(ctx, objID)

		require.NoError(t, err)
		assert.Equal(t, expectedObj, obj)
		assert.Equal(t, "system:plugin:inventory-plugin", svc.capturedSubjectID)
	})

	t.Run("propagates errors", func(t *testing.T) {
		expectedErr := world.ErrNotFound
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		obj, err := adapter.GetObject(ctx, objID)

		assert.Nil(t, obj)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error includes code and context", func(t *testing.T) {
		expectedErr := errors.New("underlying error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "obj-plugin")

		_, err := adapter.GetObject(ctx, objID)

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
		errutil.AssertErrorContext(t, err, "plugin", "obj-plugin")
		errutil.AssertErrorContext(t, err, "entity_type", "object")
	})

	// Defensive nil check: If service returns (nil, nil), treat as ErrNotFound.
	// This is a bug in the underlying service, but we handle it gracefully
	// to prevent nil pointer panics in plugins.
	t.Run("handles nil object without error", func(t *testing.T) {
		svc := &mockWorldService{object: nil, err: nil}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		obj, err := adapter.GetObject(ctx, objID)

		require.Error(t, err)
		assert.Nil(t, obj)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})
}

// blockingMockWorldService simulates slow operations that respect context cancellation.
// Methods block until the context is cancelled, then return context.DeadlineExceeded.
type blockingMockWorldService struct{}

func (m *blockingMockWorldService) GetLocation(ctx context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *blockingMockWorldService) GetCharacter(ctx context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *blockingMockWorldService) GetCharactersByLocation(ctx context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *blockingMockWorldService) GetObject(ctx context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestWorldQuerierAdapter_ContextTimeout verifies graceful handling of context
// timeouts for all query methods. The adapter should propagate context errors
// without panicking and wrap them appropriately.
func TestWorldQuerierAdapter_ContextTimeout(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	t.Run("GetLocation handles context timeout gracefully", func(t *testing.T) {
		svc := &blockingMockWorldService{}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "timeout-plugin")

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		loc, err := adapter.GetLocation(ctx, locID)

		assert.Nil(t, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})

	t.Run("GetCharacter handles context timeout gracefully", func(t *testing.T) {
		svc := &blockingMockWorldService{}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "timeout-plugin")

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		char, err := adapter.GetCharacter(ctx, charID)

		assert.Nil(t, char)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})

	t.Run("GetCharactersByLocation handles context timeout gracefully", func(t *testing.T) {
		svc := &blockingMockWorldService{}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "timeout-plugin")

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		chars, err := adapter.GetCharactersByLocation(ctx, locID, world.ListOptions{})

		assert.Nil(t, chars)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})

	t.Run("GetObject handles context timeout gracefully", func(t *testing.T) {
		svc := &blockingMockWorldService{}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "timeout-plugin")

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		obj, err := adapter.GetObject(ctx, objID)

		assert.Nil(t, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		errutil.AssertErrorCode(t, err, "PLUGIN_QUERY_FAILED")
	})
}
