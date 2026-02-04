// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package command provides the command registry, parser, and dispatch system.
package command

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
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
}

// EventBroadcaster defines the broadcast operations required by command handlers.
// This interface allows handlers to send events without depending on the concrete
// Broadcaster implementation.
type EventBroadcaster interface {
	// Broadcast sends an event to all subscribers of its stream.
	Broadcast(event core.Event)
}

// AliasRepository defines the persistence operations for alias management.
// This interface follows the "accept interfaces" Go idiom, allowing the command
// package to depend on an abstraction rather than the concrete store implementation.
type AliasRepository interface {
	// SetSystemAlias creates or updates a system-wide alias.
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
	// DeleteSystemAlias removes a system-wide alias.
	DeleteSystemAlias(ctx context.Context, alias string) error
	// SetPlayerAlias creates or updates a player-specific alias.
	SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, command string) error
	// DeletePlayerAlias removes a player-specific alias.
	DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error
}

// Compile-time interface checks to ensure concrete types implement the interfaces.
var (
	_ WorldService     = (*world.Service)(nil)
	_ EventBroadcaster = (*core.Broadcaster)(nil)
)

// CommandHandler is the function signature for command handlers.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandHandler func(ctx context.Context, exec *CommandExecution) error

// CommandEntryConfig holds the configuration for creating a CommandEntry.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntryConfig struct {
	Name         string         // canonical name (e.g., "say") - REQUIRED
	Handler      CommandHandler // Go handler or Lua dispatcher - REQUIRED
	Capabilities []string       // ALL required capabilities (AND logic)
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g., "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
}

// CommandEntry represents a registered command in the unified registry.
//
// Immutability Contract:
// CommandEntry is conceptually immutable after construction via NewCommandEntry.
// The Registry stores entries by value, so modifications to a CommandEntry
// after registration do not affect the registered command. However, callers
// SHOULD NOT modify fields after calling NewCommandEntry. Use GetCapabilities()
// to access capabilities safely (returns a defensive copy).
//
// Design Note - Why Not Compile-Time Enforcement:
// Making fields private would require getters for all fields, significantly
// increasing API surface and boilerplate. The by-value storage in Registry
// already provides implicit protection against post-registration mutation.
// Only Capabilities needs a defensive copy getter since slices are reference
// types. This pragmatic approach balances safety with Go idioms and API
// ergonomics for this codebase.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntry struct {
	Name         string         // canonical name (e.g., "say")
	Handler      CommandHandler // Go handler or Lua dispatcher
	Capabilities []string       // ALL required capabilities (AND logic) - use GetCapabilities() for safe access
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g., "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
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
func (e *CommandEntry) GetCapabilities() []string {
	if e.Capabilities == nil {
		return nil
	}
	// Preserve distinction between nil and empty slice
	result := make([]string, len(e.Capabilities))
	copy(result, e.Capabilities)
	return result
}

// NewCommandEntry creates a validated CommandEntry.
// Returns an error if Name is empty or Handler is nil.
func NewCommandEntry(cfg CommandEntryConfig) (*CommandEntry, error) {
	if cfg.Name == "" {
		return nil, oops.Code(CodeEmptyName).
			With("field", "Name").
			Errorf("Name is required")
	}
	if cfg.Handler == nil {
		return nil, oops.Code(CodeNilHandler).
			With("field", "Handler").
			Errorf("Handler is required")
	}

	return &CommandEntry{
		Name:         cfg.Name,
		Handler:      cfg.Handler,
		Capabilities: cfg.Capabilities,
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
	CharacterID   ulid.ULID  // REQUIRED: must be non-zero
	LocationID    ulid.ULID  // optional
	CharacterName string     // optional
	PlayerID      ulid.ULID  // optional
	SessionID     ulid.ULID  // optional
	Args          string     // optional
	Output        io.Writer  // REQUIRED: must be non-nil
	Services      *Services  // REQUIRED: must be non-nil
	InvokedAs     string     // optional
}

// CommandExecution provides context for command execution.
//
// Mutability Note:
// CommandExecution is mutable during dispatch. The dispatcher sets Args and
// InvokedAs after parsing. Handlers MAY read all fields but SHOULD NOT modify
// CharacterID, LocationID, PlayerID, SessionID, or Services. Modifying Args
// is allowed but rarely necessary.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandExecution struct {
	CharacterID   ulid.ULID
	LocationID    ulid.ULID
	CharacterName string
	PlayerID      ulid.ULID
	SessionID     ulid.ULID
	Args          string
	Output        io.Writer  // MUST be non-nil; command handlers write output here
	Services      *Services
	// InvokedAs is the original command name as typed by the user, before alias
	// resolution. For example, if "say'" is an alias for "say", InvokedAs will
	// be "say'" while the handler is for "say". Plugins can use this to detect
	// which variant was invoked.
	InvokedAs string
}

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
		CharacterID:   cfg.CharacterID,
		LocationID:    cfg.LocationID,
		CharacterName: cfg.CharacterName,
		PlayerID:      cfg.PlayerID,
		SessionID:     cfg.SessionID,
		Args:          cfg.Args,
		Output:        cfg.Output,
		Services:      cfg.Services,
		InvokedAs:     cfg.InvokedAs,
	}, nil
}

// Error code for service validation failures.
const (
	CodeNilService = "NIL_SERVICE"
)

// ServicesConfig holds the dependencies for constructing a Services instance.
type ServicesConfig struct {
	World       WorldService         // world model queries and mutations
	Session     core.SessionService  // session management
	Access      access.AccessControl // authorization checks
	Events      core.EventStore      // event persistence
	Broadcaster EventBroadcaster     // event broadcasting
	AliasCache  *AliasCache          // alias management (optional)
	AliasRepo   AliasRepository      // alias persistence (optional, for alias handlers)
	Registry    *Registry            // command registry (optional)
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
	world       WorldService         // world model queries and mutations
	session     core.SessionService  // session management
	access      access.AccessControl // authorization checks
	events      core.EventStore      // event persistence
	broadcaster EventBroadcaster     // event broadcasting
	aliasCache  *AliasCache          // alias management (optional, for alias commands)
	aliasRepo   AliasRepository      // alias persistence (optional, for alias handlers)
	registry    *Registry            // command registry (optional, for alias shadow detection)
}

// World returns the world service for model queries and mutations.
func (s *Services) World() WorldService { return s.world }

// Session returns the session service for session management.
func (s *Services) Session() core.SessionService { return s.session }

// Access returns the access control service for authorization checks.
func (s *Services) Access() access.AccessControl { return s.access }

// Events returns the event store for event persistence.
func (s *Services) Events() core.EventStore { return s.events }

// Broadcaster returns the event broadcaster for broadcasting events.
func (s *Services) Broadcaster() EventBroadcaster { return s.broadcaster }

// AliasCache returns the alias cache for alias management (may be nil).
func (s *Services) AliasCache() *AliasCache { return s.aliasCache }

// Registry returns the command registry for alias shadow detection (may be nil).
func (s *Services) Registry() *Registry { return s.registry }

// AliasRepo returns the alias repository for persistence (may be nil).
func (s *Services) AliasRepo() AliasRepository { return s.aliasRepo }

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
	if cfg.Access == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Access").
			Errorf("Access service is required")
	}
	if cfg.Events == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Events").
			Errorf("Events service is required")
	}
	if cfg.Broadcaster == nil {
		return nil, oops.Code(CodeNilService).
			With("service", "Broadcaster").
			Errorf("Broadcaster service is required")
	}

	return &Services{
		world:       cfg.World,
		session:     cfg.Session,
		access:      cfg.Access,
		events:      cfg.Events,
		broadcaster: cfg.Broadcaster,
		aliasCache:  cfg.AliasCache,
		aliasRepo:   cfg.AliasRepo,
		registry:    cfg.Registry,
	}, nil
}

// BroadcastSystemMessage creates and broadcasts a system event with the given message.
// This is a convenience method for handlers that need to send system messages.
// If the Broadcaster is nil, this method is a no-op.
func (s *Services) BroadcastSystemMessage(stream, message string) {
	if s.broadcaster == nil {
		return
	}

	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    stream,
		Type:      core.EventTypeSystem,
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		},
		Payload: payload,
	}

	s.broadcaster.Broadcast(event)
}

// NewTestServices creates a Services instance for testing purposes.
// Unlike NewServices, this function does not validate that required services are non-nil,
// allowing tests to create minimal Services with only the dependencies they need.
// This function should only be used in tests.
func NewTestServices(cfg ServicesConfig) *Services {
	return &Services{
		world:       cfg.World,
		session:     cfg.Session,
		access:      cfg.Access,
		events:      cfg.Events,
		broadcaster: cfg.Broadcaster,
		aliasCache:  cfg.AliasCache,
		aliasRepo:   cfg.AliasRepo,
		registry:    cfg.Registry,
	}
}
