// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

// mockWorldMutatorService implements both hostfunc.WorldService and hostfunc.WorldMutator for testing.
// This is a unified mock that handles both read and write operations.
type mockWorldMutatorService struct {
	location             *world.Location
	character            *world.Character
	characters           []*world.Character
	object               *world.Object
	err                  error
	createLocationErr    error
	createExitErr        error
	createObjectErr      error
	updateLocationErr    error
	updateObjectErr      error
	findLocationByNameFn func(ctx context.Context, subjectID, name string) (*world.Location, error)
}

// WorldService read methods
func (m *mockWorldMutatorService) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldMutatorService) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldMutatorService) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

func (m *mockWorldMutatorService) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.object, nil
}

// WorldMutator write methods
func (m *mockWorldMutatorService) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	if m.createLocationErr != nil {
		return m.createLocationErr
	}
	return nil
}

func (m *mockWorldMutatorService) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	if m.createExitErr != nil {
		return m.createExitErr
	}
	return nil
}

func (m *mockWorldMutatorService) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	if m.createObjectErr != nil {
		return m.createObjectErr
	}
	return nil
}

func (m *mockWorldMutatorService) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	if m.updateLocationErr != nil {
		return m.updateLocationErr
	}
	return nil
}

func (m *mockWorldMutatorService) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	if m.updateObjectErr != nil {
		return m.updateObjectErr
	}
	return nil
}

func (m *mockWorldMutatorService) FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error) {
	if m.findLocationByNameFn != nil {
		return m.findLocationByNameFn(ctx, subjectID, name)
	}
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldMutatorService)(nil)

// --- create_location tests ---

func TestCreateLocationFn_Success(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_location("Test Room", "A test room", "persistent")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check result is a table with expected fields
	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "expected table result")

	tbl := result.(*lua.LTable)
	assert.NotEmpty(t, tbl.RawGetString("id").String(), "expected non-empty id")
	assert.Equal(t, "Test Room", tbl.RawGetString("name").String())
}

func TestCreateLocationFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_location("Test", "", "persistent")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestCreateLocationFn_InvalidLocationType(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_location("Test", "desc", "invalid-type")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "invalid location type")
}

func TestCreateLocationFn_NoWorldService(t *testing.T) {
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.location"}))

	funcs := hostfunc.New(nil, enforcer)
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_location("Test", "", "persistent")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "world service not configured")
}

func TestCreateLocationFn_ServiceError(t *testing.T) {
	mutator := &mockWorldMutatorService{
		createLocationErr: errors.New("database connection timeout with stack trace"),
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_location("Test", "desc", "persistent")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- create_exit tests ---

func TestCreateExitFn_Success(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	fromID := ulid.Make()
	toID := ulid.Make()

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {})`, fromID, toID)
	err := L.DoString(code)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "expected table result")

	tbl := result.(*lua.LTable)
	assert.NotEmpty(t, tbl.RawGetString("id").String())
	assert.Equal(t, "north", tbl.RawGetString("name").String())
}

func TestCreateExitFn_WithBidirectionalOptions(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	fromID := ulid.Make()
	toID := ulid.Make()

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {bidirectional = true, return_name = "south"})`, fromID, toID)
	err := L.DoString(code)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
}

func TestCreateExitFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	fromID := ulid.Make()
	toID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {})`, fromID, toID)
	err := L.DoString(code)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestCreateExitFn_InvalidFromID(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	toID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.create_exit("invalid-id", "%s", "north", {})`, toID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid from_id")
}

func TestCreateExitFn_InvalidToID(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	fromID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "invalid-id", "north", {})`, fromID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid to_id")
}

func TestCreateExitFn_NoWorldService(t *testing.T) {
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	funcs := hostfunc.New(nil, enforcer)
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	fromID := ulid.Make()
	toID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {})`, fromID, toID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "world service not configured")
}

func TestCreateExitFn_ServiceError(t *testing.T) {
	mutator := &mockWorldMutatorService{
		createExitErr: errors.New("database connection timeout with stack trace"),
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.exit"}))

	fromID := ulid.Make()
	toID := ulid.Make()

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {})`, fromID, toID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- create_object tests ---

func TestCreateObjectFn_Success(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.object"}))

	locID := ulid.Make()

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.create_object("Magic Sword", {location_id = "%s"})`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "expected table result")

	tbl := result.(*lua.LTable)
	assert.NotEmpty(t, tbl.RawGetString("id").String())
	assert.Equal(t, "Magic Sword", tbl.RawGetString("name").String())
}

func TestCreateObjectFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	locID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.create_object("Sword", {location_id = "%s"})`, locID)
	err := L.DoString(code)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestCreateObjectFn_NoContainment(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.object"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.create_object("Sword", {})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "must specify exactly one containment")
}

func TestCreateObjectFn_MissingOptsTable(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.object"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	// Call create_object with only name argument, no opts table
	err := L.DoString(`result, err = holomush.create_object("Sword")`)
	require.NoError(t, err, "should not panic or error at Lua level")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "second argument must be an options table")
}

func TestCreateObjectFn_OptsNotATable(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.object"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	// Call create_object with a string instead of a table for opts
	err := L.DoString(`result, err = holomush.create_object("Sword", "not-a-table")`)
	require.NoError(t, err, "should not panic or error at Lua level")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "second argument must be an options table")
}

func TestCreateObjectFn_ServiceError(t *testing.T) {
	mutator := &mockWorldMutatorService{
		createObjectErr: errors.New("database connection timeout with stack trace"),
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.write.object"}))

	locID := ulid.Make()

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.create_object("Magic Sword", {location_id = "%s"})`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- find_location tests ---

func TestFindLocationFn_Success(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Town Square",
		Description: "The center of town",
		Type:        world.LocationTypePersistent,
	}
	mutator := &mockWorldMutatorService{
		findLocationByNameFn: func(_ context.Context, _, name string) (*world.Location, error) {
			if name == "Town Square" {
				return loc, nil
			}
			return nil, world.ErrNotFound
		},
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.read.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.find_location("Town Square")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "expected table result")

	tbl := result.(*lua.LTable)
	assert.Equal(t, locID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, "Town Square", tbl.RawGetString("name").String())
}

func TestFindLocationFn_NotFound(t *testing.T) {
	mutator := &mockWorldMutatorService{
		findLocationByNameFn: func(_ context.Context, _, _ string) (*world.Location, error) {
			return nil, world.ErrNotFound
		},
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.read.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.find_location("Nonexistent")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, "location not found", errVal.String())
}

func TestFindLocationFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.find_location("Test")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestFindLocationFn_ServiceError(t *testing.T) {
	mutator := &mockWorldMutatorService{
		findLocationByNameFn: func(_ context.Context, _, _ string) (*world.Location, error) {
			return nil, errors.New("database connection timeout with stack trace")
		},
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"world.read.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.find_location("Test Room")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- set_property tests ---

func TestSetPropertyFn_LocationDescription(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Room",
		Description: "Old description",
		Type:        world.LocationTypePersistent,
	}

	mutator := &mockWorldMutatorService{
		location: loc,
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.set_property("location", "%s", "description", "New description")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error, got: %v", errVal)

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTrue, result, "expected true result")
}

func TestSetPropertyFn_ObjectDescription(t *testing.T) {
	objID := ulid.Make()
	locID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
	require.NoError(t, err)

	mutator := &mockWorldMutatorService{
		object: obj,
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.set_property("object", "%s", "description", "A gleaming blade")`, objID)
	luaErr := L.DoString(code)
	require.NoError(t, luaErr)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")
}

func TestSetPropertyFn_InvalidEntityType(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	objID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.set_property("invalid", "%s", "description", "test")`, objID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid entity type")
}

func TestSetPropertyFn_InvalidEntityID(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.set_property("location", "invalid-id", "description", "test")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid entity_id")
}

func TestSetPropertyFn_InvalidProperty(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:   locID,
		Name: "Test Room",
		Type: world.LocationTypePersistent,
	}
	mutator := &mockWorldMutatorService{
		location: loc,
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.set_property("location", "%s", "invalid_property", "test")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid property")
}

func TestSetPropertyFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	locID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.set_property("location", "%s", "description", "test")`, locID)
	err := L.DoString(code)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestSetPropertyFn_ServiceError(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Room",
		Description: "Old description",
		Type:        world.LocationTypePersistent,
	}

	mutator := &mockWorldMutatorService{
		location:          loc,
		updateLocationErr: errors.New("database connection timeout with stack trace"),
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.set"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.set_property("location", "%s", "description", "New description")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- get_property tests ---

func TestGetPropertyFn_LocationDescription(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Room",
		Description: "A cozy room",
		Type:        world.LocationTypePersistent,
	}
	mutator := &mockWorldMutatorService{
		location: loc,
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.get"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.get_property("location", "%s", "description")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "A cozy room", result.String())
}

func TestGetPropertyFn_ObjectName(t *testing.T) {
	objID := ulid.Make()
	locID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
	require.NoError(t, err)
	obj.Description = "A gleaming blade"

	mutator := &mockWorldMutatorService{
		object: obj,
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.get"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	code := fmt.Sprintf(`result, err = holomush.get_property("object", "%s", "name")`, objID)
	luaErr := L.DoString(code)
	require.NoError(t, luaErr)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type())

	result := L.GetGlobal("result")
	assert.Equal(t, "Magic Sword", result.String())
}

func TestGetPropertyFn_InvalidEntityType(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.get"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	entityID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.get_property("invalid", "%s", "description")`, entityID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "invalid entity type")
}

func TestGetPropertyFn_EntityNotFound(t *testing.T) {
	mutator := &mockWorldMutatorService{
		err: world.ErrNotFound,
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.get"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	locID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.get_property("location", "%s", "description")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Contains(t, errVal.String(), "not found")
}

func TestGetPropertyFn_CapabilityDenied(t *testing.T) {
	mutator := &mockWorldMutatorService{}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	locID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.get_property("location", "%s", "description")`, locID)
	err := L.DoString(code)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestGetPropertyFn_ServiceError(t *testing.T) {
	mutator := &mockWorldMutatorService{
		err: errors.New("database connection timeout with stack trace"),
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"property.get"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	locID := ulid.Make()
	code := fmt.Sprintf(`result, err = holomush.get_property("location", "%s", "description")`, locID)
	err := L.DoString(code)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Error should be sanitized - verify internal details are NOT leaked
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error", "expected sanitized error message")
	assert.NotContains(t, errStr, "database", "error should not leak internal details")
	assert.NotContains(t, errStr, "connection", "error should not leak internal details")
	assert.NotContains(t, errStr, "timeout", "error should not leak internal details")
	assert.NotContains(t, errStr, "stack trace", "error should not leak internal details")
}

// --- Additional coverage tests ---

func TestWorldWriteFunctions_SubjectIDFormat(t *testing.T) {
	// Verify that subject ID is formatted correctly as "system:plugin:<name>"
	var capturedSubjectID string
	mutator := &mockWorldMutatorService{
		findLocationByNameFn: func(_ context.Context, subjectID, _ string) (*world.Location, error) {
			capturedSubjectID = subjectID
			return nil, world.ErrNotFound
		},
	}
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("my-building-plugin", []string{"world.read.location"}))

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(mutator))
	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "my-building-plugin")

	_ = L.DoString(`result, err = holomush.find_location("Test")`)

	assert.Equal(t, "system:plugin:my-building-plugin", capturedSubjectID)
}

// mockWorldServiceWithExpectations provides finer-grained control for testing.
type mockWorldServiceWithExpectations struct {
	mock.Mock
}

func (m *mockWorldServiceWithExpectations) GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error) {
	args := m.Called(ctx, subjectID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*world.Location), args.Error(1)
}

func (m *mockWorldServiceWithExpectations) GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error) {
	args := m.Called(ctx, subjectID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*world.Character), args.Error(1)
}

func (m *mockWorldServiceWithExpectations) GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error) {
	args := m.Called(ctx, subjectID, locationID, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*world.Character), args.Error(1)
}

func (m *mockWorldServiceWithExpectations) GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error) {
	args := m.Called(ctx, subjectID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*world.Object), args.Error(1)
}

func (m *mockWorldServiceWithExpectations) CreateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	args := m.Called(ctx, subjectID, loc)
	return args.Error(0)
}

func (m *mockWorldServiceWithExpectations) CreateExit(ctx context.Context, subjectID string, exit *world.Exit) error {
	args := m.Called(ctx, subjectID, exit)
	return args.Error(0)
}

func (m *mockWorldServiceWithExpectations) CreateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	args := m.Called(ctx, subjectID, obj)
	return args.Error(0)
}

func (m *mockWorldServiceWithExpectations) UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	args := m.Called(ctx, subjectID, loc)
	return args.Error(0)
}

func (m *mockWorldServiceWithExpectations) UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	args := m.Called(ctx, subjectID, obj)
	return args.Error(0)
}

func (m *mockWorldServiceWithExpectations) FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error) {
	args := m.Called(ctx, subjectID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*world.Location), args.Error(1)
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldServiceWithExpectations)(nil)
