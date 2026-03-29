// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corebuilding_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corebuilding "github.com/holomush/holomush/plugins/core-building"
)

// --- Mock ServiceProxy ---

type mockProxy struct {
	createLocationFn func(ctx context.Context, subjectID, name, desc, locType string) (*plugins.LocationResult, error)
	createExitFn     func(ctx context.Context, subjectID, fromID, toID, name string, opts plugins.CreateExitOpts) error
	queryLocationFn  func(ctx context.Context, subjectID, id string) (*plugins.LocationResult, error)
	findLocationFn   func(ctx context.Context, subjectID, name string) (*plugins.LocationResult, error)
}

func (m *mockProxy) CreateLocation(ctx context.Context, subjectID, name, desc, locType string) (*plugins.LocationResult, error) {
	if m.createLocationFn != nil {
		return m.createLocationFn(ctx, subjectID, name, desc, locType)
	}
	return &plugins.LocationResult{ID: "loc-new", Name: name}, nil
}

func (m *mockProxy) CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts plugins.CreateExitOpts) error {
	if m.createExitFn != nil {
		return m.createExitFn(ctx, subjectID, fromID, toID, name, opts)
	}
	return nil
}

func (m *mockProxy) QueryLocation(ctx context.Context, subjectID, id string) (*plugins.LocationResult, error) {
	if m.queryLocationFn != nil {
		return m.queryLocationFn(ctx, subjectID, id)
	}
	return &plugins.LocationResult{ID: id, Name: "Test Location"}, nil
}

func (m *mockProxy) FindLocation(ctx context.Context, subjectID, name string) (*plugins.LocationResult, error) {
	if m.findLocationFn != nil {
		return m.findLocationFn(ctx, subjectID, name)
	}
	return &plugins.LocationResult{ID: "loc-found", Name: name}, nil
}

// Stub implementations for remaining ServiceProxy methods.

func (m *mockProxy) QueryCharacter(context.Context, string, string) (*plugins.CharacterResult, error) {
	return nil, nil
}
func (m *mockProxy) QueryLocationCharacters(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}
func (m *mockProxy) QueryObject(context.Context, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}
func (m *mockProxy) GetCharactersByLocation(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}
func (m *mockProxy) GetObjectsByLocation(context.Context, string, string) ([]plugins.ObjectResult, error) {
	return nil, nil
}
func (m *mockProxy) CreateObject(context.Context, string, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}
func (m *mockProxy) UpdateLocation(context.Context, string, string, string, string) error { return nil }
func (m *mockProxy) UpdateCharacterDescription(context.Context, string, string, string) error {
	return nil
}
func (m *mockProxy) SetProperty(context.Context, string, string, string, string, string) error {
	return nil
}
func (m *mockProxy) GetProperty(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (m *mockProxy) FindPropertyByPrefix(context.Context, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}
func (m *mockProxy) ListPropertiesByParent(context.Context, string, string, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}
func (m *mockProxy) KVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (m *mockProxy) KVSet(context.Context, string, string, string) error    { return nil }
func (m *mockProxy) KVDelete(context.Context, string, string) error         { return nil }
func (m *mockProxy) FindSessionByName(context.Context, string) (*plugins.SessionResult, error) {
	return nil, nil
}
func (m *mockProxy) SetLastWhispered(context.Context, string, string) error { return nil }
func (m *mockProxy) DisconnectSession(context.Context, string, string) error { return nil }
func (m *mockProxy) ListActiveSessions(context.Context) ([]plugins.SessionResult, error) {
	return nil, nil
}
func (m *mockProxy) BroadcastSystemMessage(context.Context, string) error       { return nil }
func (m *mockProxy) UpdateActivity(context.Context, string) error               { return nil }
func (m *mockProxy) SetPlayerAlias(context.Context, string, string, string) error { return nil }
func (m *mockProxy) DeletePlayerAlias(context.Context, string, string) error    { return nil }
func (m *mockProxy) ListPlayerAliases(context.Context, string) ([]plugins.AliasEntry, error) {
	return nil, nil
}
func (m *mockProxy) SetSystemAlias(context.Context, string, string, string) error { return nil }
func (m *mockProxy) DeleteSystemAlias(context.Context, string) error              { return nil }
func (m *mockProxy) ListSystemAliases(context.Context) ([]plugins.AliasEntry, error) {
	return nil, nil
}
func (m *mockProxy) CheckAliasShadow(context.Context, string) (bool, string, error) {
	return false, "", nil
}
func (m *mockProxy) ListCommands(context.Context, string) ([]plugins.CommandInfo, error) {
	return nil, nil
}
func (m *mockProxy) GetCommandHelp(context.Context, string, string) (*plugins.CommandHelpInfo, error) {
	return nil, nil
}
func (m *mockProxy) EmitEvent(context.Context, string, string, []byte) error { return nil }
func (m *mockProxy) GetStartingLocationID(context.Context) (string, error)   { return "", nil }
func (m *mockProxy) Log(context.Context, string, string)                     {}

var _ plugins.ServiceProxy = (*mockProxy)(nil)

// --- Dig tests ---

func TestDig(t *testing.T) {
	handler := &corebuilding.Handler{}

	tests := []struct {
		name           string
		args           string
		proxy          *mockProxy
		wantContains   string
		wantNotContain string
	}{
		{
			name:         "empty args shows usage",
			args:         "",
			proxy:        &mockProxy{},
			wantContains: "Usage: dig",
		},
		{
			name:         "invalid args shows usage",
			args:         "north",
			proxy:        &mockProxy{},
			wantContains: "Usage: dig",
		},
		{
			name:         "basic dig without return",
			args:         `north to "Town Square"`,
			proxy:        &mockProxy{},
			wantContains: `Created "Town Square" with exit "north"`,
		},
		{
			name:         "dig with return exit",
			args:         `north to "Market" return south`,
			proxy:        &mockProxy{},
			wantContains: `and return exit "south"`,
		},
		{
			name: "create location fails",
			args: `north to "Bad Place"`,
			proxy: &mockProxy{
				createLocationFn: func(context.Context, string, string, string, string) (*plugins.LocationResult, error) {
					return nil, errors.New("db error")
				},
			},
			wantContains: "Failed to create location",
		},
		{
			name: "create exit fails",
			args: `north to "Good Place"`,
			proxy: &mockProxy{
				createExitFn: func(context.Context, string, string, string, string, plugins.CreateExitOpts) error {
					return errors.New("exit error")
				},
			},
			wantContains: "Location created but exit failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "dig",
				Args:        tt.args,
				CharacterID: "char-1",
				LocationID:  "loc-1",
			}, tt.proxy)
			require.NoError(t, err)
			assert.Contains(t, resp.Output, tt.wantContains)
			if tt.wantNotContain != "" {
				assert.NotContains(t, resp.Output, tt.wantNotContain)
			}
		})
	}
}

func TestDig_ExitOpts(t *testing.T) {
	handler := &corebuilding.Handler{}

	t.Run("bidirectional flag set for return exit", func(t *testing.T) {
		var capturedOpts plugins.CreateExitOpts
		proxy := &mockProxy{
			createExitFn: func(_ context.Context, _, _, _, _ string, opts plugins.CreateExitOpts) error {
				capturedOpts = opts
				return nil
			},
		}

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command:     "dig",
			Args:        `north to "Market" return south`,
			CharacterID: "char-1",
			LocationID:  "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Created")
		assert.True(t, capturedOpts.Bidirectional)
		assert.Equal(t, "south", capturedOpts.ReturnName)
	})

	t.Run("no bidirectional flag without return", func(t *testing.T) {
		var capturedOpts plugins.CreateExitOpts
		proxy := &mockProxy{
			createExitFn: func(_ context.Context, _, _, _, _ string, opts plugins.CreateExitOpts) error {
				capturedOpts = opts
				return nil
			},
		}

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command:     "dig",
			Args:        `north to "Town Square"`,
			CharacterID: "char-1",
			LocationID:  "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Created")
		assert.False(t, capturedOpts.Bidirectional)
		assert.Empty(t, capturedOpts.ReturnName)
	})
}

// --- Link tests ---

func TestLink(t *testing.T) {
	handler := &corebuilding.Handler{}

	tests := []struct {
		name         string
		args         string
		proxy        *mockProxy
		wantContains string
	}{
		{
			name:         "empty args shows usage",
			args:         "",
			proxy:        &mockProxy{},
			wantContains: "Usage: link",
		},
		{
			name:         "invalid args shows usage",
			args:         "east",
			proxy:        &mockProxy{},
			wantContains: "Usage: link",
		},
		{
			name: "link by ID",
			args: "east to #01ABC123",
			proxy: &mockProxy{
				queryLocationFn: func(_ context.Context, _, id string) (*plugins.LocationResult, error) {
					return &plugins.LocationResult{ID: id, Name: "Garden"}, nil
				},
			},
			wantContains: `Linked "east" to "Garden"`,
		},
		{
			name: "link by name",
			args: `east to "Garden"`,
			proxy: &mockProxy{
				findLocationFn: func(_ context.Context, _, name string) (*plugins.LocationResult, error) {
					return &plugins.LocationResult{ID: "loc-garden", Name: name}, nil
				},
			},
			wantContains: `Linked "east" to "Garden"`,
		},
		{
			name:         "link by name without quotes",
			args:         "east to Garden",
			proxy:        &mockProxy{},
			wantContains: `Linked "east" to "Garden"`,
		},
		{
			name: "target not found by ID",
			args: "east to #nonexistent",
			proxy: &mockProxy{
				queryLocationFn: func(context.Context, string, string) (*plugins.LocationResult, error) {
					return nil, errors.New("not found")
				},
			},
			wantContains: "location not found",
		},
		{
			name: "target not found by name",
			args: `east to "Nowhere"`,
			proxy: &mockProxy{
				findLocationFn: func(context.Context, string, string) (*plugins.LocationResult, error) {
					return nil, errors.New("not found")
				},
			},
			wantContains: "location not found",
		},
		{
			name: "create exit fails",
			args: "east to Garden",
			proxy: &mockProxy{
				createExitFn: func(context.Context, string, string, string, string, plugins.CreateExitOpts) error {
					return errors.New("exit error")
				},
			},
			wantContains: "Failed to create exit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "link",
				Args:        tt.args,
				CharacterID: "char-1",
				LocationID:  "loc-1",
			}, tt.proxy)
			require.NoError(t, err)
			assert.Contains(t, resp.Output, tt.wantContains)
		})
	}
}

func TestHandler_UnknownCommand(t *testing.T) {
	handler := &corebuilding.Handler{}
	resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "destroy",
		Args:    "all",
	}, &mockProxy{})
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown building command")
}
