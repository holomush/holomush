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
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Tests use pluginmocks.NewMockServiceProxy(t) from the generated mock.

// --- Describe Tests ---

func TestDescribe_Me(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "A tall figure").Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "me A tall figure",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Description set.\n", resp.Output)
}

func TestDescribe_Me_Error(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "fail").
		Return(errors.New("db error"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "describe",
		Args:        "me fail",
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to set description")
}

func TestDescribe_Here(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Description set.\n", resp.Output)
}

func TestDescribe_Target(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Description set.\n", resp.Output)
}

func TestDescribe_EmptyArgs(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "describe",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestDescribe_UnresolvableTarget(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Could not find target")
}

func TestDesc_Alias(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("UpdateCharacterDescription", mock.Anything, "char-1", "char-1", "Short").Return(nil)

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "desc",
		Args:        "me Short",
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Description set.\n", resp.Output)
}

// --- Examine Tests ---

func TestExamine_CurrentLocation(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}

func TestExamine_Here(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("QueryLocation", mock.Anything, "char-1", "loc-1").
		Return(nil, errors.New("not found"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "examine",
		Args:        "",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to examine")
}

func TestExamine_Character(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
		Args:        "Gandalf",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Gandalf")
	assert.Contains(t, resp.Output, "A wizard.")
}

func TestExamine_Object(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "don't see")
}

func TestExamine_PrefixMatch(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}

func TestCreate_Location(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}

func TestCreate_InvalidType(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
			proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("CreateObject", mock.Anything, "char-1", "Fail", "").
		Return(nil, errors.New("permission denied"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "create",
		Args:        `object "Fail"`,
		CharacterID: "char-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to create object")
}

// --- Set Tests ---

func TestSet_PropertyOnLocation(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}

func TestSet_PrefixMatch(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
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
	proxy := pluginmocks.NewMockServiceProxy(t)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "set",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestSet_InvalidSyntax(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "set",
		Args:    "description here value",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage:")
}

func TestSet_PropertyError(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindPropertyByPrefix", mock.Anything, "description").
		Return([]plugins.PropertyInfo{{Name: "description"}}, nil)
	proxy.On("SetProperty", mock.Anything, "char-1", "location", "loc-1", "description", "fail").
		Return(errors.New("access denied"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "set",
		Args:        "description of here to fail",
		CharacterID: "char-1",
		LocationID:  "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to set")
}

// --- Handler Dispatch Tests ---

func TestHandler_UnknownCommand(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	h := &Handler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "bogus",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Unknown command")
}

func TestSet_OnObject(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}

func TestSet_OnSelf(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
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
}
