// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//nolint:wrapcheck // scopedServiceProxy is a transparent delegation wrapper; re-wrapping errors adds no value.
package plugins

import (
	"context"

	"github.com/holomush/holomush/internal/content"
)

// scopedServiceProxy wraps a base ServiceProxy and overrides EmitEvent to
// include the calling plugin's name as the actor identity. All other methods
// delegate directly to the base proxy.
type scopedServiceProxy struct {
	base       ServiceProxy
	pluginName string
}

var _ ServiceProxy = (*scopedServiceProxy)(nil)

// --- World read ---

func (s *scopedServiceProxy) QueryLocation(ctx context.Context, subjectID, id string) (*LocationResult, error) {
	return s.base.QueryLocation(ctx, subjectID, id)
}

func (s *scopedServiceProxy) QueryCharacter(ctx context.Context, subjectID, id string) (*CharacterResult, error) {
	return s.base.QueryCharacter(ctx, subjectID, id)
}

func (s *scopedServiceProxy) QueryLocationCharacters(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error) {
	return s.base.QueryLocationCharacters(ctx, subjectID, locationID)
}

func (s *scopedServiceProxy) QueryObject(ctx context.Context, subjectID, id string) (*ObjectResult, error) {
	return s.base.QueryObject(ctx, subjectID, id)
}

func (s *scopedServiceProxy) FindLocation(ctx context.Context, subjectID, name string) (*LocationResult, error) {
	return s.base.FindLocation(ctx, subjectID, name)
}

func (s *scopedServiceProxy) GetCharactersByLocation(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error) {
	return s.base.GetCharactersByLocation(ctx, subjectID, locationID)
}

func (s *scopedServiceProxy) GetObjectsByLocation(ctx context.Context, subjectID, locationID string) ([]ObjectResult, error) {
	return s.base.GetObjectsByLocation(ctx, subjectID, locationID)
}

// --- World write ---

func (s *scopedServiceProxy) CreateLocation(ctx context.Context, subjectID, name, description, locationType string) (*LocationResult, error) {
	return s.base.CreateLocation(ctx, subjectID, name, description, locationType)
}

func (s *scopedServiceProxy) CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts CreateExitOpts) error {
	return s.base.CreateExit(ctx, subjectID, fromID, toID, name, opts)
}

func (s *scopedServiceProxy) CreateObject(ctx context.Context, subjectID, name, description string) (*ObjectResult, error) {
	return s.base.CreateObject(ctx, subjectID, name, description)
}

func (s *scopedServiceProxy) UpdateLocation(ctx context.Context, subjectID, id, name, description string) error {
	return s.base.UpdateLocation(ctx, subjectID, id, name, description)
}

func (s *scopedServiceProxy) UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error {
	return s.base.UpdateCharacterDescription(ctx, subjectID, characterID, description)
}

// --- Properties ---

func (s *scopedServiceProxy) SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error {
	return s.base.SetProperty(ctx, subjectID, parentType, parentID, key, value)
}

func (s *scopedServiceProxy) GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (string, error) {
	return s.base.GetProperty(ctx, subjectID, parentType, parentID, key)
}

func (s *scopedServiceProxy) FindPropertyByPrefix(ctx context.Context, prefix string) ([]PropertyInfo, error) {
	return s.base.FindPropertyByPrefix(ctx, prefix)
}

func (s *scopedServiceProxy) ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) ([]PropertyInfo, error) {
	return s.base.ListPropertiesByParent(ctx, subjectID, parentType, parentID)
}

// --- Plugin KV ---

func (s *scopedServiceProxy) KVGet(ctx context.Context, _, key string) (value string, ok bool, err error) {
	return s.base.KVGet(ctx, s.pluginName, key)
}

func (s *scopedServiceProxy) KVSet(ctx context.Context, _, key, value string) error {
	return s.base.KVSet(ctx, s.pluginName, key, value)
}

func (s *scopedServiceProxy) KVDelete(ctx context.Context, _, key string) error {
	return s.base.KVDelete(ctx, s.pluginName, key)
}

// --- Session ---

func (s *scopedServiceProxy) FindSessionByName(ctx context.Context, name string) (*SessionResult, error) {
	return s.base.FindSessionByName(ctx, name)
}

func (s *scopedServiceProxy) SetLastWhispered(ctx context.Context, sessionID, name string) error {
	return s.base.SetLastWhispered(ctx, sessionID, name)
}

func (s *scopedServiceProxy) DisconnectSession(ctx context.Context, sessionID, reason string) error {
	return s.base.DisconnectSession(ctx, sessionID, reason)
}

func (s *scopedServiceProxy) ListActiveSessions(ctx context.Context) ([]SessionResult, error) {
	return s.base.ListActiveSessions(ctx)
}

func (s *scopedServiceProxy) BroadcastSystemMessage(ctx context.Context, message string) error {
	return s.base.BroadcastSystemMessage(ctx, message)
}

func (s *scopedServiceProxy) UpdateActivity(ctx context.Context, sessionID string) error {
	return s.base.UpdateActivity(ctx, sessionID)
}

// --- Aliases ---

func (s *scopedServiceProxy) SetPlayerAlias(ctx context.Context, playerID, alias, command string) error {
	return s.base.SetPlayerAlias(ctx, playerID, alias, command)
}

func (s *scopedServiceProxy) DeletePlayerAlias(ctx context.Context, playerID, alias string) error {
	return s.base.DeletePlayerAlias(ctx, playerID, alias)
}

func (s *scopedServiceProxy) ListPlayerAliases(ctx context.Context, playerID string) ([]AliasEntry, error) {
	return s.base.ListPlayerAliases(ctx, playerID)
}

func (s *scopedServiceProxy) SetSystemAlias(ctx context.Context, alias, command, createdBy string) error {
	return s.base.SetSystemAlias(ctx, alias, command, createdBy)
}

func (s *scopedServiceProxy) DeleteSystemAlias(ctx context.Context, alias string) error {
	return s.base.DeleteSystemAlias(ctx, alias)
}

func (s *scopedServiceProxy) ListSystemAliases(ctx context.Context) ([]AliasEntry, error) {
	return s.base.ListSystemAliases(ctx)
}

func (s *scopedServiceProxy) CheckAliasShadow(ctx context.Context, alias string) (shadows bool, cmdName string, err error) {
	return s.base.CheckAliasShadow(ctx, alias)
}

// --- Commands ---

func (s *scopedServiceProxy) ListCommands(ctx context.Context, characterID string) ([]CommandInfo, error) {
	return s.base.ListCommands(ctx, characterID)
}

func (s *scopedServiceProxy) GetCommandHelp(ctx context.Context, name, characterID string) (*CommandHelpInfo, error) {
	return s.base.GetCommandHelp(ctx, name, characterID)
}

// --- Events ---

// EmitEvent overrides the base proxy to include the plugin name as actor identity.
func (s *scopedServiceProxy) EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error {
	if impl, ok := s.base.(*ServiceProxyImpl); ok {
		return impl.EmitEventAs(ctx, s.pluginName, stream, eventType, payload)
	}
	return s.base.EmitEvent(ctx, stream, eventType, payload)
}

// --- Config ---

func (s *scopedServiceProxy) GetStartingLocationID(ctx context.Context) (string, error) {
	return s.base.GetStartingLocationID(ctx)
}

// --- Content (read-only) ---

func (s *scopedServiceProxy) GetContent(ctx context.Context, key string) (*content.Item, error) {
	return s.base.GetContent(ctx, key)
}

func (s *scopedServiceProxy) ListContent(ctx context.Context, prefix string, opts content.ListOptions) (*content.ListResult, error) {
	return s.base.ListContent(ctx, prefix, opts)
}

// --- Utility ---

func (s *scopedServiceProxy) Log(ctx context.Context, level, message string) {
	s.base.Log(ctx, level, message)
}
