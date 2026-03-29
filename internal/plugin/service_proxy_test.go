// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"reflect"
	"testing"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/stretchr/testify/assert"
)

// mockServiceProxy implements ServiceProxy for compile-time verification.
type mockServiceProxy struct{}

// --- World read ---

func (m *mockServiceProxy) QueryLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) QueryCharacter(context.Context, string, string) (*plugins.CharacterResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) QueryLocationCharacters(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) QueryObject(context.Context, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) FindLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) GetCharactersByLocation(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) GetObjectsByLocation(context.Context, string, string) ([]plugins.ObjectResult, error) {
	return nil, nil
}

// --- World write ---

func (m *mockServiceProxy) CreateLocation(context.Context, string, string, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) CreateExit(context.Context, string, string, string, string, plugins.CreateExitOpts) error {
	return nil
}

func (m *mockServiceProxy) CreateObject(context.Context, string, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) UpdateLocation(context.Context, string, string, string, string) error {
	return nil
}

func (m *mockServiceProxy) UpdateCharacterDescription(context.Context, string, string, string) error {
	return nil
}

// --- Properties ---

func (m *mockServiceProxy) SetProperty(context.Context, string, string, string, string, string) error {
	return nil
}

func (m *mockServiceProxy) GetProperty(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (m *mockServiceProxy) FindPropertyByPrefix(context.Context, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

func (m *mockServiceProxy) ListPropertiesByParent(context.Context, string, string, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

// --- Plugin KV ---

func (m *mockServiceProxy) KVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (m *mockServiceProxy) KVSet(context.Context, string, string, string) error { return nil }

func (m *mockServiceProxy) KVDelete(context.Context, string, string) error { return nil }

// --- Session ---

func (m *mockServiceProxy) FindSessionByName(context.Context, string) (*plugins.SessionResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) SetLastWhispered(context.Context, string, string) error { return nil }

func (m *mockServiceProxy) DisconnectSession(context.Context, string, string) error { return nil }

func (m *mockServiceProxy) ListActiveSessions(context.Context) ([]plugins.SessionResult, error) {
	return nil, nil
}

func (m *mockServiceProxy) BroadcastSystemMessage(context.Context, string) error { return nil }

func (m *mockServiceProxy) UpdateActivity(context.Context, string) error { return nil }

// --- Aliases ---

func (m *mockServiceProxy) SetPlayerAlias(context.Context, string, string, string) error {
	return nil
}

func (m *mockServiceProxy) DeletePlayerAlias(context.Context, string, string) error { return nil }

func (m *mockServiceProxy) ListPlayerAliases(context.Context, string) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (m *mockServiceProxy) SetSystemAlias(context.Context, string, string, string) error {
	return nil
}

func (m *mockServiceProxy) DeleteSystemAlias(context.Context, string) error { return nil }

func (m *mockServiceProxy) ListSystemAliases(context.Context) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (m *mockServiceProxy) CheckAliasShadow(context.Context, string) (bool, string, error) {
	return false, "", nil
}

// --- Commands ---

func (m *mockServiceProxy) ListCommands(context.Context, string) ([]plugins.CommandInfo, error) {
	return nil, nil
}

func (m *mockServiceProxy) GetCommandHelp(context.Context, string, string) (*plugins.CommandHelpInfo, error) {
	return nil, nil
}

// --- Events ---

func (m *mockServiceProxy) EmitEvent(context.Context, string, string, []byte) error { return nil }

// --- Config ---

func (m *mockServiceProxy) GetStartingLocationID(context.Context) (string, error) { return "", nil }

// --- Utility ---

func (m *mockServiceProxy) Log(context.Context, string, string) {}

// Compile-time check: mockServiceProxy implements ServiceProxy.
var _ plugins.ServiceProxy = (*mockServiceProxy)(nil)

func TestServiceProxy_InterfaceSatisfaction(t *testing.T) {
	// The compile-time check above guarantees this, but an explicit test
	// makes failures visible in test output rather than only at build time.
	var proxy plugins.ServiceProxy = &mockServiceProxy{}
	assert.NotNil(t, proxy)
}

func TestServiceProxy_MethodCount(t *testing.T) {
	proxyType := reflect.TypeOf((*plugins.ServiceProxy)(nil)).Elem()

	// All operations from the spec's parity table plus task-specified additions:
	//   World read:  7 (QueryLocation, QueryCharacter, QueryLocationCharacters,
	//                    QueryObject, FindLocation, GetCharactersByLocation,
	//                    GetObjectsByLocation)
	//   World write: 5 (CreateLocation, CreateExit, CreateObject,
	//                    UpdateLocation, UpdateCharacterDescription)
	//   Properties:  4 (SetProperty, GetProperty, FindPropertyByPrefix,
	//                    ListPropertiesByParent)
	//   Plugin KV:   3 (KVGet, KVSet, KVDelete)
	//   Session:     6 (FindSessionByName, SetLastWhispered, DisconnectSession,
	//                    ListActiveSessions, BroadcastSystemMessage, UpdateActivity)
	//   Aliases:     7 (SetPlayerAlias, DeletePlayerAlias, ListPlayerAliases,
	//                    SetSystemAlias, DeleteSystemAlias, ListSystemAliases,
	//                    CheckAliasShadow)
	//   Commands:    2 (ListCommands, GetCommandHelp)
	//   Events:      1 (EmitEvent)
	//   Config:      1 (GetStartingLocationID)
	//   Utility:     1 (Log)
	//   Total:      37
	expectedMethods := 37
	assert.Equal(t, expectedMethods, proxyType.NumMethod(),
		"ServiceProxy method count changed — update this test and the parity table if intentional")
}

func TestServiceProxy_ExpectedMethods(t *testing.T) {
	proxyType := reflect.TypeOf((*plugins.ServiceProxy)(nil)).Elem()

	expected := []string{
		// World read
		"QueryLocation",
		"QueryCharacter",
		"QueryLocationCharacters",
		"QueryObject",
		"FindLocation",
		"GetCharactersByLocation",
		"GetObjectsByLocation",
		// World write
		"CreateLocation",
		"CreateExit",
		"CreateObject",
		"UpdateLocation",
		"UpdateCharacterDescription",
		// Properties
		"SetProperty",
		"GetProperty",
		"FindPropertyByPrefix",
		"ListPropertiesByParent",
		// Plugin KV
		"KVGet",
		"KVSet",
		"KVDelete",
		// Session
		"FindSessionByName",
		"SetLastWhispered",
		"DisconnectSession",
		"ListActiveSessions",
		"BroadcastSystemMessage",
		"UpdateActivity",
		// Aliases
		"SetPlayerAlias",
		"DeletePlayerAlias",
		"ListPlayerAliases",
		"SetSystemAlias",
		"DeleteSystemAlias",
		"ListSystemAliases",
		"CheckAliasShadow",
		// Commands
		"ListCommands",
		"GetCommandHelp",
		// Events
		"EmitEvent",
		// Config
		"GetStartingLocationID",
		// Utility
		"Log",
	}

	for _, name := range expected {
		_, ok := proxyType.MethodByName(name)
		assert.True(t, ok, "ServiceProxy missing expected method: %s", name)
	}
}
