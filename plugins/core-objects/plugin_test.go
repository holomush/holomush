// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package coreobjects

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// --- Mock ServiceProxy ---

type mockProxy struct {
	mock.Mock
}

func (m *mockProxy) QueryLocation(ctx context.Context, subjectID, id string) (*plugins.LocationResult, error) {
	args := m.Called(ctx, subjectID, id)
	if v := args.Get(0); v != nil {
		return v.(*plugins.LocationResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) QueryCharacter(ctx context.Context, subjectID, id string) (*plugins.CharacterResult, error) {
	args := m.Called(ctx, subjectID, id)
	if v := args.Get(0); v != nil {
		return v.(*plugins.CharacterResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) QueryLocationCharacters(ctx context.Context, subjectID, locationID string) ([]plugins.CharacterResult, error) {
	args := m.Called(ctx, subjectID, locationID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.CharacterResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) QueryObject(ctx context.Context, subjectID, id string) (*plugins.ObjectResult, error) {
	args := m.Called(ctx, subjectID, id)
	if v := args.Get(0); v != nil {
		return v.(*plugins.ObjectResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) FindLocation(ctx context.Context, subjectID, name string) (*plugins.LocationResult, error) {
	args := m.Called(ctx, subjectID, name)
	if v := args.Get(0); v != nil {
		return v.(*plugins.LocationResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) GetCharactersByLocation(ctx context.Context, subjectID, locationID string) ([]plugins.CharacterResult, error) {
	args := m.Called(ctx, subjectID, locationID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.CharacterResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) GetObjectsByLocation(ctx context.Context, subjectID, locationID string) ([]plugins.ObjectResult, error) {
	args := m.Called(ctx, subjectID, locationID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.ObjectResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) CreateLocation(ctx context.Context, subjectID, name, description, locationType string) (*plugins.LocationResult, error) {
	args := m.Called(ctx, subjectID, name, description, locationType)
	if v := args.Get(0); v != nil {
		return v.(*plugins.LocationResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts plugins.CreateExitOpts) error {
	args := m.Called(ctx, subjectID, fromID, toID, name, opts)
	return args.Error(0)
}

func (m *mockProxy) CreateObject(ctx context.Context, subjectID, name, description string) (*plugins.ObjectResult, error) {
	args := m.Called(ctx, subjectID, name, description)
	if v := args.Get(0); v != nil {
		return v.(*plugins.ObjectResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) UpdateLocation(ctx context.Context, subjectID, id, name, description string) error {
	args := m.Called(ctx, subjectID, id, name, description)
	return args.Error(0)
}

func (m *mockProxy) UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error {
	args := m.Called(ctx, subjectID, characterID, description)
	return args.Error(0)
}

func (m *mockProxy) SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error {
	args := m.Called(ctx, subjectID, parentType, parentID, key, value)
	return args.Error(0)
}

func (m *mockProxy) GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (string, error) {
	args := m.Called(ctx, subjectID, parentType, parentID, key)
	return args.String(0), args.Error(1)
}

func (m *mockProxy) FindPropertyByPrefix(ctx context.Context, prefix string) ([]plugins.PropertyInfo, error) {
	args := m.Called(ctx, prefix)
	if v := args.Get(0); v != nil {
		return v.([]plugins.PropertyInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) ([]plugins.PropertyInfo, error) {
	args := m.Called(ctx, subjectID, parentType, parentID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.PropertyInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) KVGet(ctx context.Context, pluginName, key string) (string, bool, error) {
	args := m.Called(ctx, pluginName, key)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *mockProxy) KVSet(ctx context.Context, pluginName, key, value string) error {
	args := m.Called(ctx, pluginName, key, value)
	return args.Error(0)
}

func (m *mockProxy) KVDelete(ctx context.Context, pluginName, key string) error {
	args := m.Called(ctx, pluginName, key)
	return args.Error(0)
}

func (m *mockProxy) FindSessionByName(ctx context.Context, name string) (*plugins.SessionResult, error) {
	args := m.Called(ctx, name)
	if v := args.Get(0); v != nil {
		return v.(*plugins.SessionResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) SetLastWhispered(ctx context.Context, sessionID, name string) error {
	args := m.Called(ctx, sessionID, name)
	return args.Error(0)
}

func (m *mockProxy) DisconnectSession(ctx context.Context, sessionID, reason string) error {
	args := m.Called(ctx, sessionID, reason)
	return args.Error(0)
}

func (m *mockProxy) ListActiveSessions(ctx context.Context) ([]plugins.SessionResult, error) {
	args := m.Called(ctx)
	if v := args.Get(0); v != nil {
		return v.([]plugins.SessionResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) BroadcastSystemMessage(ctx context.Context, message string) error {
	args := m.Called(ctx, message)
	return args.Error(0)
}

func (m *mockProxy) UpdateActivity(ctx context.Context, sessionID string) error {
	args := m.Called(ctx, sessionID)
	return args.Error(0)
}

func (m *mockProxy) SetPlayerAlias(ctx context.Context, playerID, alias, command string) error {
	args := m.Called(ctx, playerID, alias, command)
	return args.Error(0)
}

func (m *mockProxy) DeletePlayerAlias(ctx context.Context, playerID, alias string) error {
	args := m.Called(ctx, playerID, alias)
	return args.Error(0)
}

func (m *mockProxy) ListPlayerAliases(ctx context.Context, playerID string) ([]plugins.AliasEntry, error) {
	args := m.Called(ctx, playerID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.AliasEntry), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) SetSystemAlias(ctx context.Context, alias, command, createdBy string) error {
	args := m.Called(ctx, alias, command, createdBy)
	return args.Error(0)
}

func (m *mockProxy) DeleteSystemAlias(ctx context.Context, alias string) error {
	args := m.Called(ctx, alias)
	return args.Error(0)
}

func (m *mockProxy) ListSystemAliases(ctx context.Context) ([]plugins.AliasEntry, error) {
	args := m.Called(ctx)
	if v := args.Get(0); v != nil {
		return v.([]plugins.AliasEntry), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) CheckAliasShadow(ctx context.Context, alias string) (bool, string, error) {
	args := m.Called(ctx, alias)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *mockProxy) ListCommands(ctx context.Context, characterID string) ([]plugins.CommandInfo, error) {
	args := m.Called(ctx, characterID)
	if v := args.Get(0); v != nil {
		return v.([]plugins.CommandInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) GetCommandHelp(ctx context.Context, name, characterID string) (*plugins.CommandHelpInfo, error) {
	args := m.Called(ctx, name, characterID)
	if v := args.Get(0); v != nil {
		return v.(*plugins.CommandHelpInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockProxy) EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error {
	args := m.Called(ctx, stream, eventType, payload)
	return args.Error(0)
}

func (m *mockProxy) GetStartingLocationID(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *mockProxy) Log(ctx context.Context, level, message string) {
	m.Called(ctx, level, message)
}

// Compile-time check that mockProxy implements ServiceProxy.
var _ plugins.ServiceProxy = (*mockProxy)(nil)

// --- Describe Tests ---

func TestDescribe_Me(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "A tall figure").Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "me A tall figure",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, "Description set.\n", resp.Output)
	proxy.AssertExpectations(t)
}

func TestDescribe_Me_Error(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "fail").
		Return(errors.New("db error"))

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "me fail",
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Failed to set description")
}

func TestDescribe_Here(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "location", "loc-1", "description", "A dark cave").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "here A dark cave",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, "Description set.\n", resp.Output)
	proxy.AssertExpectations(t)
}

func TestDescribe_Target(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "object", "OBJ123", "description", "A gleaming blade").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "#OBJ123=A gleaming blade",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, "Description set.\n", resp.Output)
	proxy.AssertExpectations(t)
}

func TestDescribe_EmptyArgs(t *testing.T) {
	proxy := new(mockProxy)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "describe",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestDescribe_UnresolvableTarget(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "unknown=some text",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Could not find target")
}

func TestDesc_Alias(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "Short").Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "desc",
		Args:        "me Short",
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, "Description set.\n", resp.Output)
}

// --- Examine Tests ---

func TestExamine_CurrentLocation(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("QueryLocation", mock.Anything, "char-1", "loc-1").
		Return(&plugins.LocationResult{
			ID:          "loc-1",
			Name:        "The Grand Hall",
			Description: "A vast hall with marble columns.",
		}, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "location", "loc-1").
		Return([]plugins.PropertyInfo{
			{Name: "capacity", Value: "50", Visibility: "public"},
		}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "The Grand Hall")
	assert.Contains(t, resp.Output, "marble columns")
	assert.Contains(t, resp.Output, "capacity: 50")
	proxy.AssertExpectations(t)
}

func TestExamine_Here(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("QueryLocation", mock.Anything, "char-1", "loc-1").
		Return(&plugins.LocationResult{ID: "loc-1", Name: "Hall"}, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "location", "loc-1").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "here",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Hall")
}

func TestExamine_LocationError(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("QueryLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, errors.New("not found"))

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "can't examine")
}

func TestExamine_Character(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("GetCharactersByLocation", mock.Anything, "char-1", "loc-1").
		Return([]plugins.CharacterResult{
			{ID: "char-2", Name: "Gandalf", Description: "A wizard."},
		}, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "character", "char-2").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "Gandalf",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Gandalf")
	assert.Contains(t, resp.Output, "A wizard.")
}

func TestExamine_Object(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("GetCharactersByLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, nil)
	proxy.On("GetObjectsByLocation", mock.Anything, "char-1", "loc-1").
		Return([]plugins.ObjectResult{
			{ID: "obj-1", Name: "Sword", Description: "A gleaming blade."},
		}, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "object", "obj-1").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "Sword",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Sword")
	assert.Contains(t, resp.Output, "gleaming blade")
}

func TestExamine_NotFound(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("GetCharactersByLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, nil)
	proxy.On("GetObjectsByLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "Nonexistent",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "don't see")
}

func TestExamine_PrefixMatch(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("GetCharactersByLocation", mock.Anything, "char-1", "loc-1").
		Return([]plugins.CharacterResult{
			{ID: "char-2", Name: "Gandalf", Description: "A wizard."},
		}, nil)
	proxy.On("GetObjectsByLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "character", "char-2").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "Gan",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Gandalf")
}

func TestExamine_PrivatePropsFiltered(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("QueryLocation", mock.Anything, "char-1", "loc-1").
		Return(&plugins.LocationResult{ID: "loc-1", Name: "Hall"}, nil)
	proxy.On("ListPropertiesByParent", mock.Anything, "char-1", "location", "loc-1").
		Return([]plugins.PropertyInfo{
			{Name: "visible", Value: "yes", Visibility: "public"},
			{Name: "hidden", Value: "secret", Visibility: "private"},
			{Name: "admin-only", Value: "internal", Visibility: "admin"},
		}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "visible: yes")
	assert.NotContains(t, resp.Output, "secret")
	assert.NotContains(t, resp.Output, "internal")
}

// --- Create Tests ---

func TestCreate_Object(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("CreateObject", mock.Anything, "char-1", "Iron Sword", "").
		Return(&plugins.ObjectResult{ID: "obj-1", Name: "Iron Sword"}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "create",
		Args:        `object "Iron Sword"`,
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Created object")
	assert.Contains(t, resp.Output, "Iron Sword")
	assert.Contains(t, resp.Output, "obj-1")
	proxy.AssertExpectations(t)
}

func TestCreate_Location(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("CreateLocation", mock.Anything, "char-1", "Secret Room", "", "persistent").
		Return(&plugins.LocationResult{ID: "loc-2", Name: "Secret Room"}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "create",
		Args:        `location "Secret Room"`,
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Created location")
	assert.Contains(t, resp.Output, "Secret Room")
	proxy.AssertExpectations(t)
}

func TestCreate_InvalidType(t *testing.T) {
	proxy := new(mockProxy)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "create",
		Args:        `invalid "Test"`,
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "valid types: object, location")
}

func TestCreate_InvalidSyntax(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"empty args", ""},
		{"no quotes", "object Sword"},
		{"missing type", `"Sword"`},
		{"missing name", "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := new(mockProxy)
			h := &Handler{}
			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "create",
				Args:        tt.args,
				CharacterID: "char-1",
			}, proxy)

			require.NoError(t, err)
			assert.Contains(t, resp.Output, "Usage:")
		})
	}
}

func TestCreate_Object_Error(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("CreateObject", mock.Anything, "char-1", "Fail", "").
		Return(nil, errors.New("permission denied"))

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "create",
		Args:        `object "Fail"`,
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Failed to create object")
}

// --- Set Tests ---

func TestSet_PropertyOnLocation(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "location", "loc-1", "description", "A dusty room").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of here to A dusty room",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Set description of here")
	proxy.AssertExpectations(t)
}

func TestSet_PrefixMatch(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "desc").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "location", "loc-1", "description", "Updated").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "desc of here to Updated",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Set description of here")
}

func TestSet_UnknownProperty(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "bogus").
		Return(nil, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "bogus of here to value",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown property: bogus")
}

func TestSet_UnresolvableTarget(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of nobody to text",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Could not find target")
}

func TestSet_EmptyArgs(t *testing.T) {
	proxy := new(mockProxy)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "set",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestSet_InvalidSyntax(t *testing.T) {
	proxy := new(mockProxy)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "set",
		Args:    "description here value",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestSet_PropertyError(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "location", "loc-1", "description", "fail").
		Return(errors.New("access denied"))

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of here to fail",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Failed to set property")
}

// --- Handler Dispatch Tests ---

func TestHandler_UnknownCommand(t *testing.T) {
	proxy := new(mockProxy)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "bogus",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown command")
}

func TestSet_OnObject(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "object", "OBJ123", "description", "Shiny").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of #OBJ123 to Shiny",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Set description of #OBJ123")
	proxy.AssertExpectations(t)
}

func TestSet_OnSelf(t *testing.T) {
	proxy := new(mockProxy)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "character", "char-1", "description", "Updated").
		Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of me to Updated",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Set description of me")
	proxy.AssertExpectations(t)
}
