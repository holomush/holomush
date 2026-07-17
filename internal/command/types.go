// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package command provides the command registry, parser, and dispatch system.
package command

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// WorldService defines the world model operations required by command handlers.
// This interface follows the "accept interfaces" Go idiom, enabling handlers to
// depend only on the methods they actually use rather than the full world.Service.
type WorldService interface {
	// GetLocation retrieves a location by ID after checking read authorization.
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)

	// GetExitsByLocation retrieves all exits from a location after checking read authorization.
	GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error)

	// CreateExit creates a new exit after checking write authorization.
	CreateExit(ctx context.Context, subjectID string, exit *world.Exit) error

	// MoveCharacter moves a character to a new location.
	MoveCharacter(ctx context.Context, subjectID string, characterID, toLocationID ulid.ULID) error

	// GetCharacter retrieves a character by ID after checking read authorization.
	GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*world.Character, error)

	// CreateLocation creates a new location after checking write authorization.
	CreateLocation(ctx context.Context, subjectID string, loc *world.Location) error

	// UpdateLocation updates an existing location after checking write authorization.
	UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error

	// CreateObject creates a new object after checking write authorization.
	CreateObject(ctx context.Context, subjectID string, obj *world.Object) error

	// GetObject retrieves an object by ID after checking read authorization.
	GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*world.Object, error)

	// UpdateObject updates an existing object after checking write authorization.
	UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error

	// UpdateCharacterDescription sets a character's description after checking authorization.
	UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error

	// FindLocationByName searches for a location by name after checking read authorization.
	FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error)

	// GetCharactersByLocation returns characters at a location after checking authorization.
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)

	// GetObjectsByLocation returns objects at a location after checking authorization.
	GetObjectsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Object, error)

	// ListPropertiesByParent returns all properties for the given parent entity
	// after checking read authorization on the parent.
	ListPropertiesByParent(ctx context.Context, subjectID string, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error)
}

// AliasWriter defines write-only persistence operations for alias management.
// This is a narrow interface containing only the Set/Delete operations needed
// by command handlers. For the full read+write interface, see store.AliasRepository.
//
// This interface follows the "accept interfaces" Go idiom, allowing the command
// package to depend on an abstraction rather than the concrete store implementation.
// The store.PostgresAliasRepository implements both this interface and the broader
// store.AliasRepository.
type AliasWriter interface {
	// SetSystemAlias creates or updates a system-wide alias (UPSERT).
	// See store.AliasRepository.SetSystemAlias for parameter semantics;
	// createdBy is a player FK ("" for NULL) and source is a provenance
	// tag (plugin name for manifest-seeded, "sysalias" for operator-created).
	SetSystemAlias(ctx context.Context, alias, cmd, createdBy, source string) error
	// DeleteSystemAlias removes a system-wide alias.
	DeleteSystemAlias(ctx context.Context, alias string) error
	// SetPlayerAlias creates or updates a player-specific alias.
	SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, cmd string) error
	// DeletePlayerAlias removes a player-specific alias.
	DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error
}

// Compile-time interface checks to ensure concrete types implement the interfaces.
var (
	_ WorldService = (*world.Service)(nil)
)

// Scope constants define the spatial context for capability pre-flight checks.
const (
	ScopeSelf   = ""       // default — own character only
	ScopeLocal  = "local"  // current location + contents
	ScopeGlobal = "global" // server-wide
)

// validActions lists the known ABAC actions for capability validation.
//
// Thread safety: this map is initialized at package load time and never
// modified afterward. Concurrent reads without writes are safe in Go.
// CoreActions() returns a defensive copy for external mutation.
// ValidateAction() accepts a separate "known" map parameter rather
// than reading this global, keeping plugin-contributed types isolated.
var validActions = map[string]bool{
	"read": true, "write": true, "emit": true, "enter": true,
	"use": true, "delete": true, "execute": true, "admin": true,
}

// validResourceTypes lists the known ABAC resource types for capability validation.
//
// Thread safety: this map is initialized at package load time and never
// modified afterward. Concurrent reads without writes are safe in Go.
// CoreResourceTypes() returns a defensive copy for external mutation.
// ValidateResourceType() accepts a separate "known" map parameter rather
// than reading this global, keeping plugin-contributed types isolated.
var validResourceTypes = map[string]bool{
	"character": true, "location": true, "exit": true, "object": true,
	"stream": true, "property": true, "scene": true, "command": true,
	"server": true, "alias": true, "player": true, "plugin": true,
}

// validScopes lists the known scope values.
//
// Thread safety: this map is initialized at package load time and never
// modified afterward. Concurrent reads without writes are safe in Go.
var validScopes = map[string]bool{
	ScopeSelf: true, ScopeLocal: true, ScopeGlobal: true,
}

// Capability declares a resource type and action that a command will
// attempt. Used for pre-flight authorization at dispatch time.
type Capability struct {
	Action   string `yaml:"action" json:"action"`
	Resource string `yaml:"resource" json:"resource"`
	Scope    string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// Validate checks structural validity: action is non-empty, resource is non-empty,
// scope is valid. Does NOT check action or resource type membership — that requires
// cross-plugin context and is deferred to ValidateAction/ValidateResourceType at load time.
func (c Capability) Validate() error {
	if c.Action == "" {
		return oops.Code("INVALID_CAPABILITY").Errorf("action is required")
	}
	if c.Resource == "" {
		return oops.Code("INVALID_CAPABILITY").Errorf("resource is required")
	}
	if !validScopes[c.Scope] {
		return oops.Code("INVALID_CAPABILITY").
			With("scope", c.Scope).
			Errorf("unknown scope %q", c.Scope)
	}
	return nil
}

// ValidateResourceType checks that the resource type is in the provided set.
// Called during plugin load with a set that includes both core types and
// plugin-declared resource types.
func (c Capability) ValidateResourceType(known map[string]bool) error {
	if !known[c.Resource] {
		return oops.Code("INVALID_CAPABILITY").
			With("resource", c.Resource).
			Errorf("unknown resource type %q", c.Resource)
	}
	return nil
}

// CoreResourceTypes returns a defensive copy of the core resource type set.
// Used by the plugin manager to build the full known-types map.
func CoreResourceTypes() map[string]bool {
	result := make(map[string]bool, len(validResourceTypes))
	for k, v := range validResourceTypes {
		result[k] = v
	}
	return result
}

// CoreActions returns a defensive copy of the built-in action set.
// Used by the plugin manager to build the full known-actions map.
func CoreActions() map[string]bool {
	result := make(map[string]bool, len(validActions))
	for k, v := range validActions {
		result[k] = v
	}
	return result
}

// ValidateAction checks that the capability's action is in the provided set.
// Called during plugin load with a set that includes both core actions and
// plugin-declared actions.
func (c Capability) ValidateAction(known map[string]bool) error {
	if !known[c.Action] {
		return oops.Code("INVALID_CAPABILITY").
			With("action", c.Action).
			Errorf("unknown action %q", c.Action)
	}
	return nil
}

// EffectiveScope returns the scope, defaulting to ScopeSelf if empty.
func (c Capability) EffectiveScope() string {
	if c.Scope == "" {
		return ScopeSelf
	}
	return c.Scope
}

// CommandHandler is the function signature for command handlers.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandHandler func(ctx context.Context, exec *CommandExecution) error

// CommandEntryConfig holds the configuration for creating a CommandEntry.
//
// This struct is exported to allow external packages (e.g., integration tests,
// plugins) to construct CommandEntry values using the constructor.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntryConfig struct {
	Name         string         // canonical name (e.g. "say") - REQUIRED
	Handler      CommandHandler // Go handler — nil for plugin-backed commands
	PluginName   string         // non-empty for plugin-backed commands
	Capabilities []Capability   // ALL required capabilities (AND logic)
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g. "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
}

// CommandEntry represents a registered command in the unified registry.
//
// Immutability Contract:
// CommandEntry is conceptually immutable after construction via NewCommandEntry.
// The Registry stores entries by value, so modifications to a CommandEntry
// after registration do not affect the registered command. However, callers
// SHOULD NOT modify fields after calling NewCommandEntry.
//
// The handler and capabilities fields are private to enforce immutability at
// compile time. Use Handler() to access the handler and GetCapabilities() to
// access capabilities safely; GetCapabilities() returns a defensive copy.
// Other fields remain public since by-value storage in Registry already
// provides implicit protection.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntry struct {
	Name         string         // canonical name (e.g., "say")
	handler      CommandHandler // Go handler — nil for plugin-backed commands; use Handler() getter
	pluginName   string         // non-empty for plugin-backed commands; use PluginName() getter
	capabilities []Capability   // ALL required capabilities (AND logic) - use GetCapabilities() for safe access
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g., "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
}

// Handler returns the command's handler function.
// This provides read-only access to the handler after construction.
// Returns nil for plugin-backed commands.
func (e *CommandEntry) Handler() CommandHandler {
	return e.handler
}

// PluginName returns the plugin name for plugin-backed commands.
// Returns "" for compiled-in commands that use a handler function directly.
func (e *CommandEntry) PluginName() string {
	return e.pluginName
}

// Error codes for constructor validation failures.
// CodeNilServices is defined in errors.go.
const (
	CodeEmptyName  = "EMPTY_NAME"
	CodeNilHandler = "NIL_HANDLER"
	CodeZeroID     = "ZERO_ID"
	CodeNilOutput  = "NIL_OUTPUT"
)

// GetCapabilities returns a defensive copy of the command's required capabilities.
// This prevents external modification of the entry's internal state.
// Returns nil if no capabilities are set, or an empty slice if explicitly set to empty.
func (e *CommandEntry) GetCapabilities() []Capability {
	if e.capabilities == nil {
		return nil
	}
	// Preserve distinction between nil and empty slice
	result := make([]Capability, len(e.capabilities))
	copy(result, e.capabilities)
	return result
}

// NewCommandEntry creates a validated CommandEntry.
// Returns an error if Name is empty or if neither Handler nor PluginName is set.
// Handler and PluginName are mutually exclusive — setting both is an error.
func NewCommandEntry(cfg CommandEntryConfig) (*CommandEntry, error) {
	if cfg.Name == "" {
		return nil, oops.Code(CodeEmptyName).
			With("field", "Name").
			Errorf("Name is required")
	}
	if cfg.Handler == nil && cfg.PluginName == "" {
		return nil, oops.Code(CodeNilHandler).
			With("field", "Handler").
			Errorf("Handler or PluginName is required")
	}
	if cfg.Handler != nil && cfg.PluginName != "" {
		return nil, oops.Code("AMBIGUOUS_HANDLER").
			With("field", "Handler/PluginName").
			Errorf("cannot set both Handler and PluginName")
	}

	for i, cap := range cfg.Capabilities {
		if err := cap.Validate(); err != nil {
			return nil, oops.Code("INVALID_CAPABILITY").
				With("command", cfg.Name).
				With("index", i).
				Wrap(err)
		}
	}

	// Defensive copy so callers can't mutate the entry's capabilities after construction.
	var caps []Capability
	if len(cfg.Capabilities) > 0 {
		caps = make([]Capability, len(cfg.Capabilities))
		copy(caps, cfg.Capabilities)
	}

	return &CommandEntry{
		Name:         cfg.Name,
		handler:      cfg.Handler,
		pluginName:   cfg.PluginName,
		capabilities: caps,
		Help:         cfg.Help,
		Usage:        cfg.Usage,
		HelpText:     cfg.HelpText,
		Source:       cfg.Source,
	}, nil
}

// CommandExecutionConfig holds the configuration for creating a CommandExecution.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandExecutionConfig struct {
	CharacterID   ulid.ULID // REQUIRED: must be non-zero
	LocationID    ulid.ULID // optional
	CharacterName string    // optional
	PlayerID      ulid.ULID // optional
	SessionID     ulid.ULID // optional
	// ConnectionID is the ULID of the originating Connection. Phase 5
	// (holomush-5rh.14): scene focus / scene grid commands need to know
	// which connection issued the command. Zero value is accepted for
	// server-side dispatch paths that do not have a specific connection.
	ConnectionID ulid.ULID // optional
	Args         string    // optional
	Output       io.Writer // REQUIRED: must be non-nil
	Services     *Services // REQUIRED: must be non-nil
	InvokedAs    string    // optional
}

// BootedSession records a session that was forcibly terminated by a boot command.
// The gRPC layer uses this to perform leave-event and disconnect-hook teardown
// for the target, which the boot handler itself cannot do.
type BootedSession struct {
	CharacterRef core.CharacterRef
	SessionInfo  session.Info
}

// CommandExecution provides context for command execution.
//
// Immutability Contract:
// Critical fields are private with getter methods to prevent accidental modification
// by handlers. The dispatcher sets Args and InvokedAs after parsing, so these remain
// public. All other fields are set via NewCommandExecution and cannot be changed.
//
// Public fields (dispatcher sets after construction):
//   - Args: command arguments after parsing
//   - InvokedAs: original command name before alias resolution
//
// Private fields (read-only via getters):
//   - characterID, locationID, characterName, playerID, sessionID
//   - output, services
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandExecution struct {
	// Private read-only fields - use getters
	characterID   ulid.ULID
	locationID    ulid.ULID
	characterName string
	playerID      ulid.ULID
	sessionID     ulid.ULID
	// connectionID is the originating Connection. Zero for server-side
	// dispatch paths that don't have a specific connection (e.g. gRPC
	// server HandleCommand). Phase 5 (holomush-5rh.14).
	connectionID ulid.ULID
	output       io.Writer
	services     *Services

	// bootedSessions tracks sessions forcibly ended by admin boot.
	// After dispatch, the server layer processes these for leave events and hooks.
	bootedSessions []BootedSession

	// endSession signals that the invoking session should end (e.g. quit command).
	endSession bool

	// responseIsError is set by the dispatcher when a plugin command returns
	// CommandError or CommandFailure status. The gRPC server reads this to
	// decide whether to emit command_response or command_error events.
	responseIsError bool

	// Public fields - dispatcher sets these after construction
	Args string
	// InvokedAs is the original command name as typed by the user, before alias
	// resolution. Handlers can use this to alter behavior based on which alias
	// invoked them. For example, the pose handler checks InvokedAs == ";" to
	// distinguish no-space pose (;'s eyes glow → "Alaric's eyes glow") from
	// standard pose (: waves → "Alaric waves"). This pattern allows a single
	// registered command to serve multiple alias variants without separate
	// command registrations.
	InvokedAs string
}

// CharacterID returns the executing character's ID.
func (e *CommandExecution) CharacterID() ulid.ULID { return e.characterID }

// LocationID returns the character's current location ID.
func (e *CommandExecution) LocationID() ulid.ULID { return e.locationID }

// CharacterName returns the executing character's name.
func (e *CommandExecution) CharacterName() string { return e.characterName }

// PlayerID returns the player's ID (account owner of the character).
func (e *CommandExecution) PlayerID() ulid.ULID { return e.playerID }

// SessionID returns the session ID for the current connection.
func (e *CommandExecution) SessionID() ulid.ULID { return e.sessionID }

// ConnectionID returns the originating connection ID. Zero value for
// server-side dispatch paths that don't have a specific connection.
func (e *CommandExecution) ConnectionID() ulid.ULID { return e.connectionID }

// Output returns the writer for command output. MUST be non-nil.
func (e *CommandExecution) Output() io.Writer { return e.output }

// Services returns the service dependencies for command handlers.
func (e *CommandExecution) Services() *Services { return e.services }

// RecordBootedSession records a session that was forcibly terminated by a boot
// command so the server layer can emit leave events and run disconnect hooks.
func (e *CommandExecution) RecordBootedSession(bs BootedSession) {
	e.bootedSessions = append(e.bootedSessions, bs)
}

// BootedSessions returns sessions that were forcibly terminated during this
// command execution. Returns nil when no sessions were booted.
func (e *CommandExecution) BootedSessions() []BootedSession { return e.bootedSessions }

// SetEndSession signals that the invoking session should end.
func (e *CommandExecution) SetEndSession(v bool) { e.endSession = v }

// EndSession returns true if the invoking session should end.
func (e *CommandExecution) EndSession() bool { return e.endSession }

// SetResponseIsError marks the response as an error (CommandError or CommandFailure).
// The gRPC server reads this to choose command_error vs command_response event type.
func (e *CommandExecution) SetResponseIsError(v bool) { e.responseIsError = v }

// ResponseIsError returns true if the plugin handler returned an error status.
func (e *CommandExecution) ResponseIsError() bool { return e.responseIsError }

// NewCommandExecution creates a validated CommandExecution.
// Returns an error if CharacterID is zero, Services is nil, or Output is nil.
func NewCommandExecution(cfg CommandExecutionConfig) (*CommandExecution, error) {
	if cfg.CharacterID.IsZero() {
		return nil, oops.Code(CodeZeroID).
			With("field", "CharacterID").
			Errorf("CharacterID is required and must be non-zero")
	}
	if cfg.Services == nil {
		return nil, oops.Code(CodeNilServices).
			With("field", "Services").
			Errorf("Services is required")
	}
	if cfg.Output == nil {
		return nil, oops.Code(CodeNilOutput).
			With("field", "Output").
			Errorf("Output is required")
	}

	return &CommandExecution{
		characterID:   cfg.CharacterID,
		locationID:    cfg.LocationID,
		characterName: cfg.CharacterName,
		playerID:      cfg.PlayerID,
		sessionID:     cfg.SessionID,
		connectionID:  cfg.ConnectionID,
		Args:          cfg.Args,
		output:        cfg.Output,
		services:      cfg.Services,
		InvokedAs:     cfg.InvokedAs,
	}, nil
}

// Error code for service validation failures.
const (
	CodeNilService = "NIL_SERVICE"
)

// ServicesConfig holds the dependencies for constructing a Services instance.
type ServicesConfig struct {
	World              WorldService             // world model queries and mutations
	Session            session.Access           // session management
	Engine             types.AccessPolicyEngine // ABAC policy engine for authorization
	Events             core.EventAppender       // event persistence
	AliasCache         *AliasCache              // alias management (optional)
	AliasRepo          AliasWriter              // alias persistence (optional, for alias handlers)
	Registry           *Registry                // command registry (optional)
	PropertyRegistry   *property.Registry       // property registry (optional)
	StartingLocationID ulid.ULID                // default starting location for home fallback (optional)
}

// Services provides access to core services for command handlers.
//
// Immutability Contract:
// Services is immutable after construction via NewServices. All fields are
// private with getter methods to enforce compile-time immutability.
// Handlers MUST access services only through exec.Services getters within
// the command handler's execution context. The Services struct is shared
// across all command executions.
type Services struct {
	world              WorldService             // world model queries and mutations
	session            session.Access           // session management
	engine             types.AccessPolicyEngine // ABAC policy engine for authorization
	events             core.EventAppender       // event persistence
	aliasCache         *AliasCache              // alias management (optional, for alias commands)
	aliasRepo          AliasWriter              // alias persistence (optional, for alias handlers)
	registry           *Registry                // command registry (optional, for alias shadow detection)
	propertyRegistry   *property.Registry       // property registry (optional, for property handlers)
	startingLocationID ulid.ULID                // default starting location for home fallback
}

// World returns the world service for model queries and mutations.
func (s *Services) World() WorldService { return s.world }

// Session returns the session service for session management.
func (s *Services) Session() session.Access { return s.session }

// Engine returns the ABAC policy engine for authorization checks.
func (s *Services) Engine() types.AccessPolicyEngine { return s.engine }

// Events returns the event appender for event persistence.
func (s *Services) Events() core.EventAppender { return s.events }

// AliasCache returns the alias cache for alias management (may be nil).
func (s *Services) AliasCache() *AliasCache { return s.aliasCache }

// Registry returns the command registry for alias shadow detection (may be nil).
func (s *Services) Registry() *Registry { return s.registry }

// AliasRepo returns the alias writer for persistence (may be nil).
func (s *Services) AliasRepo() AliasWriter { return s.aliasRepo }

// PropertyRegistry returns the property registry (may be nil).
func (s *Services) PropertyRegistry() *property.Registry { return s.propertyRegistry }

// StartingLocationID returns the default starting location ID used as a fallback
// when a character has no home property set. Returns zero value if not configured.
func (s *Services) StartingLocationID() ulid.ULID { return s.startingLocationID }

// NewServices creates a validated Services instance.
// Returns an error if any required service is nil.
func NewServices(cfg ServicesConfig) (*Services, error) {
	if cfg.World == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "World").
			Errorf("World service is required")
	}
	if cfg.Session == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Session").
			Errorf("Session service is required")
	}
	if cfg.Engine == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Engine").
			Errorf("Engine service is required")
	}
	if cfg.Events == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Events").
			Errorf("Events service is required")
	}
	if cfg.PropertyRegistry == nil {
		cfg.PropertyRegistry = property.SharedRegistry()
	}

	return &Services{
		world:              cfg.World,
		session:            cfg.Session,
		engine:             cfg.Engine,
		events:             cfg.Events,
		aliasCache:         cfg.AliasCache,
		aliasRepo:          cfg.AliasRepo,
		registry:           cfg.Registry,
		propertyRegistry:   cfg.PropertyRegistry,
		startingLocationID: cfg.StartingLocationID,
	}, nil
}

// BroadcastSystemMessage creates and appends a system event with the given message.
// This is a convenience method for handlers that need to send system messages.
// If the event store is nil, this method logs a debug message and returns.
func (s *Services) BroadcastSystemMessage(ctx context.Context, stream, message string) {
	if s.events == nil {
		slog.DebugContext(ctx, "broadcastSystemMessage: event store not configured")
		return
	}

	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})

	event := core.NewEvent(stream, eventvocab.EventTypeSystem, core.Actor{
		Kind: core.ActorSystem,
		ID:   core.ActorSystemID,
	}, payload)

	if err := s.events.Append(ctx, event); err != nil {
		slog.WarnContext(ctx, "broadcastSystemMessage: failed to append event",
			"stream", stream, "error", err)
	}
}

// NewTestServices creates a Services instance for testing purposes.
// Unlike NewServices, this function does not validate that required services are non-nil,
// allowing tests to create minimal Services with only the dependencies they need.
// This function should only be used in tests.
func NewTestServices(cfg ServicesConfig) *Services {
	if cfg.PropertyRegistry == nil {
		cfg.PropertyRegistry = property.SharedRegistry()
	}
	return &Services{
		world:              cfg.World,
		session:            cfg.Session,
		engine:             cfg.Engine,
		events:             cfg.Events,
		aliasCache:         cfg.AliasCache,
		aliasRepo:          cfg.AliasRepo,
		registry:           cfg.Registry,
		propertyRegistry:   cfg.PropertyRegistry,
		startingLocationID: cfg.StartingLocationID,
	}
}

// NewTestEntry creates a CommandEntry for testing purposes.
// Unlike NewCommandEntry, this function does not validate required fields,
// allowing tests to create entries without a handler. This is useful for
// mock registries in external test packages.
// This function should only be used in tests.
func NewTestEntry(cfg CommandEntryConfig) CommandEntry {
	return CommandEntry{
		Name:         cfg.Name,
		handler:      cfg.Handler,
		pluginName:   cfg.PluginName,
		capabilities: cfg.Capabilities,
		Help:         cfg.Help,
		Usage:        cfg.Usage,
		HelpText:     cfg.HelpText,
		Source:       cfg.Source,
	}
}

// NewTestExecution creates a CommandExecution instance for testing purposes.
// Unlike NewCommandExecution, this function does not validate required fields,
// allowing tests to create minimal executions with only the fields they need.
// This function should only be used in tests.
func NewTestExecution(cfg CommandExecutionConfig) *CommandExecution {
	return &CommandExecution{
		characterID:   cfg.CharacterID,
		locationID:    cfg.LocationID,
		characterName: cfg.CharacterName,
		playerID:      cfg.PlayerID,
		sessionID:     cfg.SessionID,
		connectionID:  cfg.ConnectionID,
		Args:          cfg.Args,
		output:        cfg.Output,
		services:      cfg.Services,
		InvokedAs:     cfg.InvokedAs,
	}
}
