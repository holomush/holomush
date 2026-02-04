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

// Compile-time interface checks to ensure concrete types implement the interfaces.
var (
	_ WorldService     = (*world.Service)(nil)
	_ EventBroadcaster = (*core.Broadcaster)(nil)
)

// CommandHandler is the function signature for command handlers.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandHandler func(ctx context.Context, exec *CommandExecution) error

// CommandEntry represents a registered command in the unified registry.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntry struct {
	Name         string         // canonical name (e.g., "say")
	Handler      CommandHandler // Go handler or Lua dispatcher
	Capabilities []string       // ALL required capabilities (AND logic)
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g., "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
}

// CommandExecution provides context for command execution.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandExecution struct {
	CharacterID   ulid.ULID
	LocationID    ulid.ULID
	CharacterName string
	PlayerID      ulid.ULID
	SessionID     ulid.ULID
	Args          string
	Output        io.Writer
	Services      *Services
	// InvokedAs is the original command name as typed by the user, before alias
	// resolution. For example, if "say'" is an alias for "say", InvokedAs will
	// be "say'" while the handler is for "say". Plugins can use this to detect
	// which variant was invoked.
	InvokedAs string
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
}

// Services provides access to core services for command handlers.
// Handlers MUST NOT store references to services beyond execution.
// Handlers MUST access services only through exec.Services.
type Services struct {
	World       WorldService         // world model queries and mutations
	Session     core.SessionService  // session management
	Access      access.AccessControl // authorization checks
	Events      core.EventStore      // event persistence
	Broadcaster EventBroadcaster     // event broadcasting
}

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
		World:       cfg.World,
		Session:     cfg.Session,
		Access:      cfg.Access,
		Events:      cfg.Events,
		Broadcaster: cfg.Broadcaster,
	}, nil
}

// BroadcastSystemMessage creates and broadcasts a system event with the given message.
// This is a convenience method for handlers that need to send system messages.
// If the Broadcaster is nil, this method is a no-op.
func (s *Services) BroadcastSystemMessage(stream, message string) {
	if s.Broadcaster == nil {
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

	s.Broadcaster.Broadcast(event)
}
