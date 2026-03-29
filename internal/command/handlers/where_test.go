// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

func TestWhereHandler_NoSessions(t *testing.T) {
	player := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess()

	services := testutil.NewServicesBuilder().WithSession(mockSA).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "0 characters online")
}

func TestWhereHandler_ShowsVisibleCharactersAndLocations(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	loc1ID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: char1ID, LocationID: loc1ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char2ID, LocationID: loc1ID, Status: session.StatusActive},
	)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Gandalf"}
	char2 := &world.Character{ID: char2ID, PlayerID: playerID, Name: "Legolas"}
	loc1 := &world.Location{ID: loc1ID, Name: "The Grand Hall"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char2ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char2ID).
		Return(char2, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(loc1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, loc1ID).
		Return(loc1, nil).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Gandalf")
	assert.Contains(t, output, "The Grand Hall")
	assert.Contains(t, output, "2 characters online")
}

func TestWhereHandler_FiltersABACInvisibleCharacters(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	loc1ID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: char1ID, LocationID: loc1ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char2ID, LocationID: loc1ID, Status: session.StatusActive},
	)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Gandalf"}
	loc1 := &world.Location{ID: loc1ID, Name: "The Grand Hall"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	// char1 is visible
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	// char2 is denied
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char2ID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil)

	// location lookup for char1
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(loc1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, loc1ID).
		Return(loc1, nil).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Gandalf")
	assert.Contains(t, output, "1 character online")
}

func TestWhereHandler_LocationPermissionDeniedShowsPrivate(t *testing.T) {
	charID := ulid.Make()
	locID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: charID, LocationID: locID, Status: session.StatusActive},
	)

	char := &world.Character{ID: charID, PlayerID: playerID, Name: "Frodo"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	// character is visible
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(charID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, charID).
		Return(char, nil)

	// location is denied
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(locID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil)

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Frodo")
	assert.Contains(t, output, "[Private]")
}

func TestWhereHandler_SortsByLocationThenCharacterName(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	char3ID := ulid.Make()
	char4ID := ulid.Make()
	loc1ID := ulid.Make()
	loc2ID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: char1ID, LocationID: loc2ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char2ID, LocationID: loc1ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char3ID, LocationID: loc1ID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char4ID, LocationID: loc2ID, Status: session.StatusActive},
	)

	chars := map[ulid.ULID]*world.Character{
		char1ID: {ID: char1ID, PlayerID: playerID, Name: "Sam"},
		char2ID: {ID: char2ID, PlayerID: playerID, Name: "Frodo"},
		char3ID: {ID: char3ID, PlayerID: playerID, Name: "Legolas"},
		char4ID: {ID: char4ID, PlayerID: playerID, Name: "Gandalf"},
	}
	loc1 := &world.Location{ID: loc1ID, Name: "The Shire"}
	loc2 := &world.Location{ID: loc2ID, Name: "The Grand Hall"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	for charID, char := range chars {
		charIDCopy := charID
		charCopy := char
		fixture.Mocks.Engine.EXPECT().
			Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(charIDCopy.String())}).
			Return(types.NewDecision(types.EffectAllow, "", ""), nil)
		fixture.Mocks.CharacterRepo.EXPECT().
			Get(mock.Anything, charIDCopy).
			Return(charCopy, nil)
	}

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(loc1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, loc1ID).
		Return(loc1, nil).Maybe()

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(loc2ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, loc2ID).
		Return(loc2, nil).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	// All characters and locations should be present
	assert.Contains(t, output, "Frodo")
	assert.Contains(t, output, "Legolas")
	assert.Contains(t, output, "Gandalf")
	assert.Contains(t, output, "Sam")
	assert.Contains(t, output, "The Shire")
	assert.Contains(t, output, "The Grand Hall")
	assert.Contains(t, output, "4 characters online")

	// Verify sort order: The Grand Hall (G) before The Shire (S)
	grandHallPos := indexOfString(output, "The Grand Hall")
	shirePos := indexOfString(output, "The Shire")
	assert.True(t, grandHallPos < shirePos, "The Grand Hall should appear before The Shire")

	// Gandalf (G) should appear before Sam (S) within The Grand Hall
	gandalfPos := indexOfString(output, "Gandalf")
	samPos := indexOfString(output, "Sam")
	assert.True(t, gandalfPos < samPos, "Gandalf should appear before Sam")
}

func TestWhereHandler_CircuitBreakerTripsOnEngineErrors(t *testing.T) {
	executor := testutil.RegularPlayer()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	// Create 5 sessions — circuit breaker trips after 3 engine errors
	charIDs := make([]ulid.ULID, 5)
	sessions := make([]*session.Info, 5)
	for i := range charIDs {
		charIDs[i] = ulid.Make()
		sessions[i] = &session.Info{
			ID:          ulid.Make().String(),
			CharacterID: charIDs[i],
			Status:      session.StatusActive,
		}
	}
	mockSA := testutil.NewMockSessionAccess(sessions...)

	fixture := testutil.NewWorldServiceBuilder(t).Build()

	// All character lookups return engine errors
	for _, charID := range charIDs {
		charIDCopy := charID
		fixture.Mocks.Engine.EXPECT().
			Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(charIDCopy.String())}).
			Return(types.NewDecision(types.EffectDeny, "", ""), world.ErrAccessEvaluationFailed).Maybe()
	}

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	// Output should show error notice
	output := buf.String()
	assert.Contains(t, output, "could not be displayed")
}

func TestWhereHandler_SkipsCharacterNotFound(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	locID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: char1ID, LocationID: locID, Status: session.StatusActive},
		&session.Info{ID: ulid.Make().String(), CharacterID: char2ID, LocationID: locID, Status: session.StatusActive},
	)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Visible"}
	loc := &world.Location{ID: locID, Name: "Test Hall"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	// char2 access allowed but not found (stale session)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(char2ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char2ID).
		Return(nil, world.ErrNotFound)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(locID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, locID).
		Return(loc, nil).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Visible")
	assert.Contains(t, output, "1 character online")
}

func TestWhereHandler_WarnsOnUnexpectedErrors(t *testing.T) {
	errorCharID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: errorCharID, Status: session.StatusActive},
	)

	unexpectedErr := errors.New("database timeout")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(errorCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr)

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "could not be displayed")
}

func TestWhereHandler_LocationLookupFailureShowsPrivate(t *testing.T) {
	charID := ulid.Make()
	locID := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	mockSA := testutil.NewMockSessionAccess(
		&session.Info{ID: ulid.Make().String(), CharacterID: charID, LocationID: locID, Status: session.StatusActive},
	)

	char := &world.Character{ID: charID, PlayerID: playerID, Name: "Bilbo"}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(executor.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.CharacterResource(charID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, charID).
		Return(char, nil)

	// location lookup returns unexpected error — treated as [Private]
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: access.LocationResource(locID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, locID).
		Return(nil, world.ErrNotFound)

	services := testutil.NewServicesBuilder().
		WithSession(mockSA).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhereHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Bilbo")
	assert.Contains(t, output, "[Private]")
}

// indexOfString is a helper to find the byte position of a substring.
func indexOfString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
