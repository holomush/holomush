// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package session provides types and interfaces for persistent game sessions.
// Sessions track a character's ongoing presence, survive disconnects, and
// support replay of missed events on reconnect.
package session

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Status represents the lifecycle state of a session.
type Status string

// Session status constants.
const (
	StatusActive   Status = "active"
	StatusDetached Status = "detached"
	StatusExpired  Status = "expired"
)

// IsValid returns true if the status is a recognized value.
func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusDetached, StatusExpired:
		return true
	default:
		return false
	}
}

// String returns the status as a string.
func (s Status) String() string {
	return string(s)
}

// Info contains all state for a persistent game session.
type Info struct {
	ID             string
	CharacterID    ulid.ULID
	CharacterName  string
	LocationID     ulid.ULID
	IsGuest        bool
	Status         Status
	GridPresent    bool
	EventCursors   map[string]ulid.ULID
	CommandHistory []string
	TTLSeconds     int
	MaxHistory     int
	DetachedAt     *time.Time
	ExpiresAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IsActive returns true if the session is in active status.
func (i *Info) IsActive() bool {
	return i.Status == StatusActive
}

// IsExpired returns true if the session has passed its expiry time.
func (i *Info) IsExpired() bool {
	if i.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*i.ExpiresAt)
}

// Connection represents a single client attached to a session.
type Connection struct {
	ID          ulid.ULID
	SessionID   string
	ClientType  string   // "terminal", "comms_hub", "telnet"
	Streams     []string // event streams this connection subscribes to
	ConnectedAt time.Time
}

// EventType enumerates session lifecycle events.
type EventType int

const (
	// Destroyed signals the session was destroyed (quit, kick, reap).
	Destroyed EventType = iota
)

// Event signals a lifecycle change for a watched session.
type Event struct {
	Type    EventType
	Message string
}

// Store manages persistent session state. Implementations MUST be
// safe for concurrent use.
type Store interface {
	// Get retrieves a session by ID.
	Get(ctx context.Context, id string) (*Info, error)

	// Set creates or updates a session.
	Set(ctx context.Context, id string, info *Info) error

	// Delete removes a session. The reason string propagates to any
	// active WatchSession watchers before the session is removed.
	Delete(ctx context.Context, id string, reason string) error

	// WatchSession returns a channel that receives an Event when
	// the session is destroyed. The channel is closed after the event
	// is delivered.
	WatchSession(ctx context.Context, sessionID string) (<-chan Event, error)

	// FindByCharacter returns the active or detached session for a character.
	FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// ListByPlayer returns all non-expired sessions for a player's characters.
	// TODO: filter by playerID when player-character relationship table exists.
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*Info, error)

	// ListExpired returns all sessions past their expiry time.
	ListExpired(ctx context.Context) ([]*Info, error)

	// UpdateStatus transitions a session's status.
	UpdateStatus(ctx context.Context, id string, status Status,
		detachedAt *time.Time, expiresAt *time.Time) error

	// ReattachCAS atomically transitions a detached session to active.
	// Returns true if the row was updated, false if another client won the race.
	ReattachCAS(ctx context.Context, id string) (bool, error)

	// UpdateCursors updates the event cursors for a session.
	UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error

	// AppendCommand adds a command to the session's history, enforcing the cap.
	AppendCommand(ctx context.Context, id string, command string, maxHistory int) error

	// GetCommandHistory returns the session's command history.
	GetCommandHistory(ctx context.Context, id string) ([]string, error)

	// AddConnection registers a new connection to a session.
	AddConnection(ctx context.Context, conn *Connection) error

	// RemoveConnection removes a connection from a session.
	RemoveConnection(ctx context.Context, connectionID ulid.ULID) error

	// CountConnections returns the number of active connections for a session.
	CountConnections(ctx context.Context, sessionID string) (int, error)

	// CountConnectionsByType returns the number of active connections of a
	// specific client type for a session.
	CountConnectionsByType(ctx context.Context, sessionID string, clientType string) (int, error)

	// UpdateGridPresent sets the grid_present flag on a session.
	UpdateGridPresent(ctx context.Context, id string, present bool) error

	// ListActiveByLocation returns active sessions whose LocationID matches.
	// Used for presence lists — "who is connected at this location?"
	ListActiveByLocation(ctx context.Context, locationID ulid.ULID) ([]*Info, error)
}
