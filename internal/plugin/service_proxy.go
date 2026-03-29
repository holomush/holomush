// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "context"

// ServiceProxy is the API contract between plugins and the host runtime.
// Both Lua and Go plugins use this interface to access game services.
//
// The subjectID parameter on world and property operations is the character's
// ID (from CommandRequest.CharacterID), used for ABAC authorization. It
// identifies who is performing the action, not which plugin is making the call.
// The plugin host (LocalPluginHost, gRPC adapter) supplies this from the
// command context — plugins do not set it themselves.
type ServiceProxy interface {
	// --- World read ---

	// QueryLocation retrieves a location by ID.
	QueryLocation(ctx context.Context, subjectID, id string) (*LocationResult, error)

	// QueryCharacter retrieves a character by ID.
	QueryCharacter(ctx context.Context, subjectID, id string) (*CharacterResult, error)

	// QueryLocationCharacters returns all characters present at a location.
	QueryLocationCharacters(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error)

	// QueryObject retrieves an object by ID.
	QueryObject(ctx context.Context, subjectID, id string) (*ObjectResult, error)

	// FindLocation searches for a location by name.
	FindLocation(ctx context.Context, subjectID, name string) (*LocationResult, error)

	// GetCharactersByLocation returns characters at a location with pagination.
	GetCharactersByLocation(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error)

	// GetObjectsByLocation returns objects at a location.
	GetObjectsByLocation(ctx context.Context, subjectID, locationID string) ([]ObjectResult, error)

	// --- World write ---

	// CreateLocation creates a new location.
	CreateLocation(ctx context.Context, subjectID, name, description, locationType string) (*LocationResult, error)

	// CreateExit creates an exit between two locations.
	CreateExit(ctx context.Context, subjectID, fromID, toID, name string, opts CreateExitOpts) error

	// CreateObject creates a new object at a location.
	CreateObject(ctx context.Context, subjectID, name, description string) (*ObjectResult, error)

	// UpdateLocation updates an existing location's name and description.
	UpdateLocation(ctx context.Context, subjectID, id, name, description string) error

	// UpdateCharacterDescription sets a character's description.
	UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error

	// --- Properties ---

	// SetProperty sets a property on an entity.
	SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error

	// GetProperty retrieves a property value from an entity.
	GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (string, error)

	// FindPropertyByPrefix returns properties matching a name prefix.
	FindPropertyByPrefix(ctx context.Context, prefix string) ([]PropertyInfo, error)

	// ListPropertiesByParent returns all properties for an entity.
	ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) ([]PropertyInfo, error)

	// --- Plugin KV ---

	// KVGet retrieves a plugin key-value pair. Returns the value and whether the key exists.
	KVGet(ctx context.Context, pluginName, key string) (string, bool, error)

	// KVSet stores a plugin key-value pair.
	KVSet(ctx context.Context, pluginName, key, value string) error

	// KVDelete removes a plugin key-value pair.
	KVDelete(ctx context.Context, pluginName, key string) error

	// --- Session ---

	// FindSessionByName finds an active session by character name (case-insensitive).
	FindSessionByName(ctx context.Context, name string) (*SessionResult, error)

	// SetLastWhispered records the last whisper target on a session.
	SetLastWhispered(ctx context.Context, sessionID, name string) error

	// DisconnectSession forcibly disconnects a session with a reason.
	DisconnectSession(ctx context.Context, sessionID, reason string) error

	// ListActiveSessions returns all currently active sessions.
	ListActiveSessions(ctx context.Context) ([]SessionResult, error)

	// BroadcastSystemMessage sends a system message to all active sessions.
	BroadcastSystemMessage(ctx context.Context, message string) error

	// UpdateActivity bumps the last-activity timestamp on a session.
	UpdateActivity(ctx context.Context, sessionID string) error

	// --- Aliases ---

	// SetPlayerAlias creates or updates a player alias.
	// The playerID identifies the player (account owner), not the character.
	SetPlayerAlias(ctx context.Context, playerID, alias, command string) error

	// DeletePlayerAlias removes a player alias.
	// The playerID identifies the player (account owner), not the character.
	DeletePlayerAlias(ctx context.Context, playerID, alias string) error

	// ListPlayerAliases returns all aliases for a player.
	// The playerID identifies the player (account owner), not the character.
	ListPlayerAliases(ctx context.Context, playerID string) ([]AliasEntry, error)

	// SetSystemAlias creates or updates a system-wide alias.
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error

	// DeleteSystemAlias removes a system-wide alias.
	DeleteSystemAlias(ctx context.Context, alias string) error

	// ListSystemAliases returns all system-wide aliases.
	ListSystemAliases(ctx context.Context) ([]AliasEntry, error)

	// CheckAliasShadow checks whether an alias shadows an existing command.
	// Returns true and the shadowed command name if a shadow exists.
	CheckAliasShadow(ctx context.Context, alias string) (bool, string, error)

	// --- Commands ---

	// ListCommands returns commands available to a character.
	ListCommands(ctx context.Context, characterID string) ([]CommandInfo, error)

	// GetCommandHelp returns detailed help for a command.
	GetCommandHelp(ctx context.Context, name, characterID string) (*CommandHelpInfo, error)

	// --- Events ---

	// EmitEvent emits an event to a stream.
	EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error

	// --- Config ---

	// GetStartingLocationID returns the server's configured starting location.
	GetStartingLocationID(ctx context.Context) (string, error)

	// --- Utility ---

	// Log writes a log message at the given level (debug, info, warn, error).
	Log(ctx context.Context, level, message string)
}

// LocationResult carries location data across the plugin SDK boundary.
// Uses string IDs rather than ulid.ULID for simplicity at the SDK layer.
type LocationResult struct {
	ID          string
	Name        string
	Description string
	Type        string
	OwnerID     string // empty if unowned
}

// CharacterResult carries character data across the plugin SDK boundary.
type CharacterResult struct {
	ID          string
	PlayerID    string
	Name        string
	Description string
	LocationID  string // empty if not in world
}

// ObjectResult carries object data across the plugin SDK boundary.
type ObjectResult struct {
	ID          string
	Name        string
	Description string
	LocationID  string // empty if not at a location
	OwnerID     string // empty if unowned
}

// SessionResult carries session data across the plugin SDK boundary.
type SessionResult struct {
	ID            string
	CharacterID   string
	CharacterName string
	LocationID    string
	Status        string
	GridPresent   bool
	LastWhispered string
}

// AliasEntry carries alias data across the plugin SDK boundary.
type AliasEntry struct {
	Alias   string
	Command string
}

// CommandInfo carries command metadata across the plugin SDK boundary.
type CommandInfo struct {
	Name   string
	Help   string // short description
	Source string // "core" or plugin name
}

// CommandHelpInfo carries detailed command help across the plugin SDK boundary.
type CommandHelpInfo struct {
	Name     string
	Help     string // short description
	Usage    string // usage pattern
	HelpText string // detailed markdown help
	Source   string
}

// PropertyInfo carries property data across the plugin SDK boundary.
type PropertyInfo struct {
	ID         string
	ParentType string
	ParentID   string
	Name       string
	Value      string // empty string for flag-style properties
	Visibility string
}

// CreateExitOpts holds optional parameters for exit creation.
type CreateExitOpts struct {
	Bidirectional bool
	ReturnName    string
	Aliases       []string
}
