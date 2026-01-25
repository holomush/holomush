// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

// mockWorldService implements hostfunc.WorldService for testing.
type mockWorldService struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
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

func (m *mockWorldService) GetCharactersByLocation(_ context.Context, subjectID string, _ ulid.ULID) ([]*world.Character, error) {
	m.capturedSubjectID = subjectID
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

// Compile-time interface check.
var _ hostfunc.WorldService = (*mockWorldService)(nil)

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

		chars, err := adapter.GetCharactersByLocation(ctx, locID)

		require.NoError(t, err)
		assert.Equal(t, expectedChars, chars)
		assert.Equal(t, "system:plugin:presence-plugin", svc.capturedSubjectID)
	})

	t.Run("returns empty slice", func(t *testing.T) {
		svc := &mockWorldService{characters: []*world.Character{}}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID)

		require.NoError(t, err)
		assert.Empty(t, chars)
	})

	t.Run("propagates errors", func(t *testing.T) {
		expectedErr := errors.New("database error")
		svc := &mockWorldService{err: expectedErr}
		adapter := hostfunc.NewWorldQuerierAdapter(svc, "test-plugin")

		chars, err := adapter.GetCharactersByLocation(ctx, locID)

		assert.Nil(t, chars)
		assert.ErrorIs(t, err, expectedErr)
	})
}
