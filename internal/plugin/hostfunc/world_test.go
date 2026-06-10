// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

// mockWorldQuerier implements hostfunc.WorldMutator for testing.
// It provides both read and write operations for world state.
type mockWorldQuerier struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
	object     *world.Object
	err        error
}

// WorldMutator read methods (with subjectID for ABAC)
func (m *mockWorldQuerier) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldQuerier) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldQuerier) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

func (m *mockWorldQuerier) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.object, nil
}

// WorldMutator write methods
func (m *mockWorldQuerier) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldQuerier) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (m *mockWorldQuerier) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldQuerier) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldQuerier) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldQuerier) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldQuerier)(nil)

// newWorldTestLuaState creates a lua.LState with hostfunc registered against querier.
// Pass nil querier to simulate "world service not configured".
// The LState is closed via t.Cleanup — callers must not defer L.Close separately.
func newWorldTestLuaState(t *testing.T, querier hostfunc.WorldMutator) *lua.LState {
	t.Helper()
	var funcs *hostfunc.Functions
	if querier != nil {
		funcs = hostfunc.New(nil, hostfunc.WithWorldService(querier))
	} else {
		funcs = hostfunc.New(nil)
	}
	L := lua.NewState()
	t.Cleanup(L.Close)
	funcs.Register(L, "test-plugin")
	return L
}

// assertInternalErrorRef validates that errVal is a sanitized "internal error (ref: <ulid>)" message.
func assertInternalErrorRef(t *testing.T, errVal lua.LValue) {
	t.Helper()
	require.Equal(t, lua.LTString, errVal.Type())
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error (ref: ", "expected sanitized error message with reference ID")
	assert.NotContains(t, errStr, "database", "internal error details should not be exposed")

	const prefix = "internal error (ref: "
	const suffix = ")"
	require.True(t, len(errStr) >= len(prefix)+len(suffix)+26, "error message too short for ULID")
	refID := errStr[len(prefix) : len(errStr)-len(suffix)]
	assert.Len(t, refID, 26, "reference ID should be a 26-character ULID")
	_, parseErr := ulid.Parse(refID)
	assert.NoError(t, parseErr, "reference ID should be a valid ULID")
}

func TestQueryLocation(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Location",
		Description: "A test location for testing",
		Type:        world.LocationTypePersistent,
	}

	L := newWorldTestLuaState(t, &mockWorldQuerier{location: loc})

	err := L.DoString(`location, err = holomush.query_location("` + locID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check location is a table with expected fields
	luaLoc := L.GetGlobal("location")
	require.Equal(t, lua.LTTable, luaLoc.Type(), "expected table result")

	tbl := luaLoc.(*lua.LTable)
	assert.Equal(t, locID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, loc.Name, tbl.RawGetString("name").String())
	assert.Equal(t, loc.Description, tbl.RawGetString("description").String())
	assert.Equal(t, string(loc.Type), tbl.RawGetString("type").String())
}

func TestQueryCharacter(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	locID := ulid.Make()
	char := &world.Character{
		ID:          charID,
		PlayerID:    playerID,
		Name:        "Test Character",
		Description: "A brave adventurer with a mysterious past.",
		LocationID:  &locID,
	}

	L := newWorldTestLuaState(t, &mockWorldQuerier{character: char})

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check character is a table with expected fields
	character := L.GetGlobal("character")
	require.Equal(t, lua.LTTable, character.Type(), "expected table result")

	tbl := character.(*lua.LTable)
	assert.Equal(t, charID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, playerID.String(), tbl.RawGetString("player_id").String())
	assert.Equal(t, char.Name, tbl.RawGetString("name").String())
	assert.Equal(t, char.Description, tbl.RawGetString("description").String())
	assert.Equal(t, locID.String(), tbl.RawGetString("location_id").String())
}

func TestQueryCharacterNilLocation(t *testing.T) {
	charID := ulid.Make()
	char := &world.Character{
		ID:         charID,
		Name:       "Test Character",
		LocationID: nil, // Not in world
	}

	L := newWorldTestLuaState(t, &mockWorldQuerier{character: char})

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	require.Equal(t, lua.LTTable, character.Type())

	tbl := character.(*lua.LTable)
	locID := tbl.RawGetString("location_id")
	assert.Equal(t, lua.LTNil, locID.Type(), "expected nil location_id")
}

func TestQueryLocationCharacters(t *testing.T) {
	locationID := ulid.Make()
	char1 := &world.Character{
		ID:   ulid.Make(),
		Name: "Alice",
	}
	char2 := &world.Character{
		ID:   ulid.Make(),
		Name: "Bob",
	}

	L := newWorldTestLuaState(t, &mockWorldQuerier{characters: []*world.Character{char1, char2}})

	err := L.DoString(`characters, err = holomush.query_location_characters("` + locationID.String() + `")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	characters := L.GetGlobal("characters")
	require.Equal(t, lua.LTTable, characters.Type(), "expected table result")

	tbl := characters.(*lua.LTable)
	assert.Equal(t, 2, tbl.Len(), "expected 2 characters")

	// Check first character
	first := tbl.RawGetInt(1)
	require.Equal(t, lua.LTTable, first.Type())
	firstTbl := first.(*lua.LTable)
	assert.Equal(t, char1.ID.String(), firstTbl.RawGetString("id").String())
	assert.Equal(t, char1.Name, firstTbl.RawGetString("name").String())

	// Check second character
	second := tbl.RawGetInt(2)
	require.Equal(t, lua.LTTable, second.Type())
	secondTbl := second.(*lua.LTable)
	assert.Equal(t, char2.ID.String(), secondTbl.RawGetString("id").String())
	assert.Equal(t, char2.Name, secondTbl.RawGetString("name").String())
}

func TestQueryLocationCharactersEmptyLocation(t *testing.T) {
	locationID := ulid.Make()

	L := newWorldTestLuaState(t, &mockWorldQuerier{characters: []*world.Character{}})

	err := L.DoString(`characters, err = holomush.query_location_characters("` + locationID.String() + `")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type())

	characters := L.GetGlobal("characters")
	require.Equal(t, lua.LTTable, characters.Type())

	tbl := characters.(*lua.LTable)
	assert.Equal(t, 0, tbl.Len(), "expected empty table")
}

func TestQueryObject(t *testing.T) {
	objID := ulid.Make()
	locID := ulid.Make()
	ownerID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
	require.NoError(t, err)
	obj.Description = "A glowing blade of ancient power."
	obj.OwnerID = &ownerID

	L := newWorldTestLuaState(t, &mockWorldQuerier{object: obj})

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check obj is a table with expected fields
	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type(), "expected table result")

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, objID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, obj.Name, tbl.RawGetString("name").String())
	assert.Equal(t, obj.Description, tbl.RawGetString("description").String())
	assert.Equal(t, lua.LFalse, tbl.RawGetString("is_container"))
	assert.Equal(t, locID.String(), tbl.RawGetString("location_id").String())
	assert.Equal(t, ownerID.String(), tbl.RawGetString("owner_id").String())
	assert.Equal(t, "location", tbl.RawGetString("containment_type").String())
}

func TestQueryObjectWithContainer(t *testing.T) {
	objID := ulid.Make()
	containerID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Gold Coins", world.ContainedInObject(containerID))
	require.NoError(t, err)
	obj.Description = "A pile of shiny gold coins."
	obj.IsContainer = true

	L := newWorldTestLuaState(t, &mockWorldQuerier{object: obj})

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, lua.LTrue, tbl.RawGetString("is_container"))
	assert.Equal(t, containerID.String(), tbl.RawGetString("contained_in_object_id").String())
	assert.Equal(t, "object", tbl.RawGetString("containment_type").String())
}

func TestQueryObjectHeldByCharacter(t *testing.T) {
	objID := ulid.Make()
	charID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.HeldByCharacter(charID))
	require.NoError(t, err)
	obj.Description = "A glowing blade."

	L := newWorldTestLuaState(t, &mockWorldQuerier{object: obj})

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, charID.String(), tbl.RawGetString("held_by_character_id").String())
	assert.Equal(t, "character", tbl.RawGetString("containment_type").String())
}

// TestQueryObjectNilOptionalFields tests the host function's defensive handling of nil
// optional fields when returning object data to Lua plugins.
//
// NOTE: This test intentionally creates an Object with invalid state (no containment set)
// to verify the host function gracefully handles nil containment fields. In production,
// objects are always created via NewObjectWithID() which requires containment, so this
// invalid state should never occur. This test ensures plugins won't crash if they somehow
// receive an object in an unexpected state.
func TestQueryObjectNilOptionalFields(t *testing.T) {
	objID := ulid.Make()
	obj := &world.Object{
		ID:          objID,
		Name:        "Simple Object",
		Description: "Nothing special.",
		IsContainer: false,
		// Intentionally leaving containment unset to test defensive nil handling.
		// This is NOT a valid production state - see function comment above.
	}

	L := newWorldTestLuaState(t, &mockWorldQuerier{object: obj})

	err := L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, lua.LTNil, tbl.RawGetString("location_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("held_by_character_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("contained_in_object_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("owner_id").Type())
}

// TestQueryFunctionErrorPaths exercises the error-path families for all four query
// functions (query_location, query_character, query_location_characters, query_object)
// in a single parameterized table. Each row captures per-function variation
// (lua snippet, result variable name, invalid-ID message, not-found message)
// while the common assertions (InternalError, PermissionDenied, NoQuerier) apply
// uniformly to every row.
func TestQueryFunctionErrorPaths(t *testing.T) {
	type queryFn struct {
		name        string // display name for subtests
		invalidSnip string // Lua snippet using an invalid ID literal
		validSnip   string // Lua snippet using a valid ULID (ulid.Make().String() will be substituted)
		resultVar   string // global variable name the function sets on success/failure
		invalidMsg  string // expected error substring for an invalid-format ID
		notFoundMsg string // expected exact error string for ErrNotFound
	}

	fns := []queryFn{
		{
			name:        "query_location",
			invalidSnip: `location, err = holomush.query_location("not-a-valid-ulid")`,
			validSnip:   `location, err = holomush.query_location("VALID_ID")`,
			resultVar:   "location",
			invalidMsg:  "invalid location ID",
			notFoundMsg: "location not found",
		},
		{
			name:        "query_character",
			invalidSnip: `character, err = holomush.query_character("not-valid")`,
			validSnip:   `character, err = holomush.query_character("VALID_ID")`,
			resultVar:   "character",
			invalidMsg:  "invalid character ID",
			notFoundMsg: "character not found",
		},
		{
			name:        "query_location_characters",
			invalidSnip: `characters, err = holomush.query_location_characters("invalid")`,
			validSnip:   `characters, err = holomush.query_location_characters("VALID_ID")`,
			resultVar:   "characters",
			invalidMsg:  "invalid location ID",
			notFoundMsg: "location not found",
		},
		{
			name:        "query_object",
			invalidSnip: `obj, err = holomush.query_object("not-valid-ulid")`,
			validSnip:   `obj, err = holomush.query_object("VALID_ID")`,
			resultVar:   "obj",
			invalidMsg:  "invalid object ID",
			notFoundMsg: "object not found",
		},
	}

	for _, fn := range fns {
		fn := fn // capture loop var
		t.Run(fn.name, func(t *testing.T) {
			t.Run("returns error for invalid ID format", func(t *testing.T) {
				L := newWorldTestLuaState(t, &mockWorldQuerier{})
				require.NoError(t, L.DoString(fn.invalidSnip))

				result := L.GetGlobal(fn.resultVar)
				errVal := L.GetGlobal("err")
				assert.Equal(t, lua.LTNil, result.Type(), "expected nil result for invalid ID")
				assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
				assert.Contains(t, errVal.String(), fn.invalidMsg)
			})

			t.Run("returns sanitized error when resource is not found", func(t *testing.T) {
				L := newWorldTestLuaState(t, &mockWorldQuerier{err: world.ErrNotFound})
				snip := substituteValidID(fn.validSnip)
				require.NoError(t, L.DoString(snip))

				result := L.GetGlobal(fn.resultVar)
				errVal := L.GetGlobal("err")
				assert.Equal(t, lua.LTNil, result.Type(), "expected nil result for not found")
				assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
				assert.Equal(t, fn.notFoundMsg, errVal.String(), "expected sanitized not-found message")
			})

			t.Run("returns sanitized error with reference ID for internal errors", func(t *testing.T) {
				// Internal errors must be sanitized — plugin should not see "database error"
				L := newWorldTestLuaState(t, &mockWorldQuerier{err: errors.New("database error connection timeout with stack trace")})
				snip := substituteValidID(fn.validSnip)
				require.NoError(t, L.DoString(snip))

				result := L.GetGlobal(fn.resultVar)
				errVal := L.GetGlobal("err")
				assert.Equal(t, lua.LTNil, result.Type())
				assertInternalErrorRef(t, errVal)
			})

			t.Run("returns access denied for permission errors", func(t *testing.T) {
				L := newWorldTestLuaState(t, &mockWorldQuerier{err: world.ErrPermissionDenied})
				snip := substituteValidID(fn.validSnip)
				require.NoError(t, L.DoString(snip))

				result := L.GetGlobal(fn.resultVar)
				errVal := L.GetGlobal("err")
				assert.Equal(t, lua.LTNil, result.Type())
				assert.Equal(t, lua.LTString, errVal.Type())
				assert.Equal(t, "access denied", errVal.String(), "expected sanitized access denied message")
			})

			t.Run("returns error when world service is not configured", func(t *testing.T) {
				L := newWorldTestLuaState(t, nil) // nil = no world service
				snip := substituteValidID(fn.validSnip)
				require.NoError(t, L.DoString(snip))

				result := L.GetGlobal(fn.resultVar)
				errVal := L.GetGlobal("err")
				assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
				assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
				assert.Contains(t, errVal.String(), "world service not configured - contact server administrator")
			})
		})
	}
}

// substituteValidID replaces the VALID_ID placeholder in a Lua snippet with a fresh ULID.
func substituteValidID(snip string) string {
	return strings.ReplaceAll(snip, "VALID_ID", ulid.Make().String())
}

// TestQueryLocationTimeout verifies that context.DeadlineExceeded is surfaced
// to plugins as "operation timed out" (unique to query_location; other queries
// share the same error-mapping path but only location has this explicit test).
func TestQueryLocationTimeout(t *testing.T) {
	locationID := ulid.Make()
	// Context timeout should be surfaced to plugins as "operation timed out"
	L := newWorldTestLuaState(t, &mockWorldQuerier{err: context.DeadlineExceeded})

	err := L.DoString(`location, err = holomush.query_location("` + locationID.String() + `")`)
	require.NoError(t, err)

	loc := L.GetGlobal("location")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, loc.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "operation timed out", errVal.String(), "expected timeout error message")
}

// contextAwareWorldQuerier passes through the context to allow testing context propagation.
type contextAwareWorldQuerier struct {
	ctxChan chan context.Context // receives the context passed to queries
	err     error                // error to return
}

// WorldMutator read methods (with subjectID for ABAC)
func (m *contextAwareWorldQuerier) GetLocation(ctx context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Location{ID: ulid.Make(), Name: "Test"}, nil
}

func (m *contextAwareWorldQuerier) GetCharacter(ctx context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Character{ID: ulid.Make(), Name: "Test"}, nil
}

func (m *contextAwareWorldQuerier) GetCharactersByLocation(ctx context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return []*world.Character{}, nil
}

func (m *contextAwareWorldQuerier) GetObject(ctx context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Object{ID: ulid.Make(), Name: "Test"}, nil
}

// WorldMutator write methods
func (m *contextAwareWorldQuerier) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *contextAwareWorldQuerier) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (m *contextAwareWorldQuerier) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *contextAwareWorldQuerier) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *contextAwareWorldQuerier) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *contextAwareWorldQuerier) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*contextAwareWorldQuerier)(nil)

func TestQueryLocationInheritsParentContext(t *testing.T) {
	// Create a parent context with a custom value to verify inheritance
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	L.SetContext(parentCtx) // Set the parent context on the Lua state

	err := L.DoString(`location, err = holomush.query_location("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// The context passed to the querier should inherit from the Lua state's context
	select {
	case receivedCtx := <-ctxChan:
		// Verify the context inherits values from the parent
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryCharacterInheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	L.SetContext(parentCtx)

	err := L.DoString(`char, err = holomush.query_character("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryLocationCharactersInheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	L.SetContext(parentCtx)

	err := L.DoString(`chars, err = holomush.query_location_characters("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryObjectInheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	L.SetContext(parentCtx)

	err := L.DoString(`obj, err = holomush.query_object("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryLocationInheritsContextDeadline(t *testing.T) {
	// Create a context with a short deadline (10ms) - shorter than the 5s default
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	L.SetContext(ctx)

	err := L.DoString(`location, err = holomush.query_location("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// Verify the derived context respects the parent's deadline
	select {
	case receivedCtx := <-ctxChan:
		deadline, ok := receivedCtx.Deadline()
		require.True(t, ok, "derived context should have a deadline")
		// The deadline should be within ~10ms of now (the parent's deadline)
		// rather than 5 seconds (the default query timeout)
		assert.WithinDuration(t, time.Now(), deadline, 50*time.Millisecond,
			"deadline should inherit from parent context, not use the 5s default")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryLocationFallbackToBackgroundContext(t *testing.T) {
	// Test that when Lua state has no context set, we fall back to context.Background()
	// This ensures backwards compatibility

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	L := newWorldTestLuaState(t, querier)
	// Note: NOT calling L.SetContext() - context is nil

	err := L.DoString(`location, err = holomush.query_location("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// The query should still work and use a 5-second timeout derived from Background
	select {
	case receivedCtx := <-ctxChan:
		deadline, ok := receivedCtx.Deadline()
		require.True(t, ok, "derived context should have a deadline from default timeout")
		// Deadline should be roughly 5 seconds from now (the default query timeout)
		assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 100*time.Millisecond,
			"should use default 5s timeout when no parent context set")
	default:
		t.Fatal("querier was not called")
	}

	// Query should succeed
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "query should succeed with fallback context")
}
