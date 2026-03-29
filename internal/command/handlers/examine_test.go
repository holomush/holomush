// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/world"
)

// grantLocationExamine configures mock expectations for examining the current location.
func grantLocationExamine(fixture *testutil.WorldServiceFixture, subjectID string, loc *world.Location,
	exits []*world.Exit, props []*world.EntityProperty,
) {
	locID := loc.ID
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "location:" + locID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil).Times(2)
	fixture.Mocks.LocationRepo.EXPECT().Get(mock.Anything, locID).Return(loc, nil)
	fixture.Mocks.ExitRepo.EXPECT().ListFromLocation(mock.Anything, locID).Return(exits, nil)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "property:location:" + locID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.PropertyRepo.EXPECT().ListByParent(mock.Anything, "location", locID).Return(props, nil)
}

// grantTargetResolve configures mock expectations for resolving a named target.
func grantTargetResolve(fixture *testutil.WorldServiceFixture, subjectID string, locID ulid.ULID,
	chars []*world.Character, exits []*world.Exit, objs []*world.Object,
) {
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "list_characters", Resource: "location:" + locID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		GetByLocation(mock.Anything, locID, world.ListOptions{}).Return(chars, nil)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "location:" + locID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().ListFromLocation(mock.Anything, locID).Return(exits, nil)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "list_objects", Resource: "location:" + locID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ObjectRepo.EXPECT().ListAtLocation(mock.Anything, locID).Return(objs, nil)
}

func buildExamineServices(fixture *testutil.WorldServiceFixture, engine types.AccessPolicyEngine) *command.Services {
	return testutil.NewServicesBuilder().WithWorldFixture(fixture).WithEngine(engine).Build()
}

func TestExamineHandler_PlayerSeesNameDesc(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Grand Hall", "A vast hall with marble columns.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	ownerID := ulid.Make()
	loc.OwnerID = &ownerID
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	val := "50"
	props := []*world.EntityProperty{
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "capacity", Value: &val, Visibility: "public"},
	}
	grantLocationExamine(fixture, subjectID, loc, nil, props)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Name:")
	assert.Contains(t, output, "The Grand Hall")
	assert.Contains(t, output, "Description:")
	assert.NotContains(t, output, "Owner:")
	assert.NotContains(t, output, "ULID:")
}

func TestExamineHandler_BuilderSeesOwnerType(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	loc := testutil.NewRoom("The Grand Hall", "A vast hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	ownerID := ulid.Make()
	loc.OwnerID = &ownerID
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantLocationExamine(fixture, subjectID, loc, nil, nil)
	engine := policytest.NewGrantEngine()
	engine.Grant(subjectID, "execute", "build.examine")
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Owner:")
	assert.Contains(t, output, "Type:")
	assert.NotContains(t, output, "ULID:")
}

func TestExamineHandler_AdminSeesEverything(t *testing.T) {
	player := testutil.AdminPlayer()
	loc := testutil.NewRoom("The Grand Hall", "A vast hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	ownerID := ulid.Make()
	loc.OwnerID = &ownerID
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	sysVal := "internal"
	props := []*world.EntityProperty{
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "sys.mode", Value: &sysVal, Visibility: "system"},
	}
	grantLocationExamine(fixture, subjectID, loc, nil, props)
	engine := policytest.NewGrantEngine()
	engine.Grant(subjectID, "execute", "admin.examine")
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "ULID:")
	assert.Contains(t, output, "Owner:")
	assert.Contains(t, output, "Type:")
}

func TestExamineHandler_ExamineCharacter(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	gandalf := &world.Character{
		ID: ulid.Make(), PlayerID: ulid.Make(), Name: "Gandalf",
		Description: "A wise wizard.", LocationID: &loc.ID, CreatedAt: time.Now(),
	}
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantTargetResolve(fixture, subjectID, loc.ID, []*world.Character{gandalf}, nil, nil)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "property:character:" + gandalf.ID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.PropertyRepo.EXPECT().ListByParent(mock.Anything, "character", gandalf.ID).Return(nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("Gandalf").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Name:")
	assert.Contains(t, output, "Gandalf")
	assert.Contains(t, output, "Description:")
}

func TestExamineHandler_TargetNotFound(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantTargetResolve(fixture, subjectID, loc.ID, nil, nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("Nobody").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "I don't see \"Nobody\" here.")
}

func TestExamineHandler_AmbiguousMatch(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	gandalf := &world.Character{
		ID: ulid.Make(), PlayerID: ulid.Make(), Name: "Gandalf",
		LocationID: &loc.ID, CreatedAt: time.Now(),
	}
	galadriel := &world.Character{
		ID: ulid.Make(), PlayerID: ulid.Make(), Name: "Galadriel",
		LocationID: &loc.ID, CreatedAt: time.Now(),
	}
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantTargetResolve(fixture, subjectID, loc.ID, []*world.Character{gandalf, galadriel}, nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("G").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Multiple matches for \"G\"")
	assert.Contains(t, output, "Gandalf")
	assert.Contains(t, output, "Galadriel")
}

func TestExamineHandler_ExamineHere(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantLocationExamine(fixture, subjectID, loc, nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("here").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Hall")
}

func TestExamineHandler_ExamineExitByName(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	toLoc := testutil.NewRoom("The Library", "Books.")
	exit, err := world.NewExitWithID(ulid.Make(), loc.ID, toLoc.ID, "North")
	require.NoError(t, err)
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantTargetResolve(fixture, subjectID, loc.ID, nil, []*world.Exit{exit}, nil)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "property:exit:" + exit.ID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.PropertyRepo.EXPECT().ListByParent(mock.Anything, "exit", exit.ID).Return(nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("North").WithServices(services).Build()
	err = ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "North")
}

func TestExamineHandler_ExamineObject(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	obj, err := world.NewObjectWithID(ulid.Make(), "Sword", world.InLocation(loc.ID))
	require.NoError(t, err)
	obj.Description = "A gleaming blade."
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantTargetResolve(fixture, subjectID, loc.ID, nil, nil, []*world.Object{obj})
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject: subjectID, Action: "read", Resource: "property:object:" + obj.ID.String(),
		}).Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.PropertyRepo.EXPECT().ListByParent(mock.Anything, "object", obj.ID).Return(nil, nil)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("Sword").WithServices(services).Build()
	err = ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Sword")
	assert.Contains(t, output, "A gleaming blade.")
}

func TestExamineHandler_PropertyVisibility(t *testing.T) {
	player := testutil.RegularPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	pubVal := "50"
	privVal := "secret-stuff"
	sysVal := "internal"
	props := []*world.EntityProperty{
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "capacity", Value: &pubVal, Visibility: "public"},
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "password", Value: &privVal, Visibility: "private"},
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "sys.mode", Value: &sysVal, Visibility: "system"},
	}
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantLocationExamine(fixture, subjectID, loc, nil, props)
	engine := policytest.NewGrantEngine()
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "capacity")
	assert.NotContains(t, output, "password")
	assert.NotContains(t, output, "sys.mode")
}

func TestExamineHandler_AdminSeesAllProperties(t *testing.T) {
	player := testutil.AdminPlayer()
	loc := testutil.NewRoom("The Hall", "A hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	ownerID := ulid.Make()
	loc.OwnerID = &ownerID
	pubVal := "50"
	sysVal := "internal"
	adminVal := "admin-only"
	props := []*world.EntityProperty{
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "capacity", Value: &pubVal, Visibility: "public"},
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "sys.mode", Value: &sysVal, Visibility: "system"},
		{ID: ulid.Make(), ParentType: "location", ParentID: loc.ID, Name: "debug.flag", Value: &adminVal, Visibility: "admin"},
	}
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantLocationExamine(fixture, subjectID, loc, nil, props)
	engine := policytest.NewGrantEngine()
	engine.Grant(subjectID, "execute", "admin.examine")
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err := ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "capacity")
	assert.Contains(t, output, "sys.mode")
	assert.Contains(t, output, "debug.flag")
}

func TestExamineHandler_BuilderSeesExitDestinations(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	loc := testutil.NewRoom("The Hall", "A hall.")
	loc.CreatedAt = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	toLoc := testutil.NewRoom("The Library", "Books.")
	exit, err := world.NewExitWithID(ulid.Make(), loc.ID, toLoc.ID, "North")
	require.NoError(t, err)
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	grantLocationExamine(fixture, subjectID, loc, []*world.Exit{exit}, nil)
	engine := policytest.NewGrantEngine()
	engine.Grant(subjectID, "execute", "build.examine")
	services := buildExamineServices(fixture, engine)
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).WithLocation(loc).WithArgs("").WithServices(services).Build()
	err = ExamineHandler(context.Background(), exec)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "North")
	assert.Contains(t, output, toLoc.ID.String())
}
