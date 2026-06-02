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

// FocusKind enumerates the types of focused contexts a character can
// participate in. The substrate dispatches replay policy by Kind via the
// FocusKindPolicy interface (see internal/grpc/focus). Adding a new kind
// requires: (a) a new constant here, (b) a new FocusKindPolicy
// implementation registered in the coordinator's constructor, and (c)
// corresponding plugin proto enum value.
type FocusKind string

const (
	// FocusKindScene marks a focus membership as a scene participation.
	// Streams derived by ScenePolicy: "events.<gid>.scene.<id>.ic" and "events.<gid>.scene.<id>.ooc".
	FocusKindScene FocusKind = "scene"
)

// FocusKey uniquely identifies a focus membership within a session. Two
// memberships with the same FocusKey are forbidden by invariant I-1
// (Focus Membership Uniqueness).
type FocusKey struct {
	Kind     FocusKind
	TargetID ulid.ULID
}

// LeaveByTargetResult summarizes a cross-session focus-leave sweep. See
// focus.Coordinator.LeaveFocusByTarget for the full contract. The type
// lives in session because it must be referenced both by the focus
// package (which imports session) and by the hostfunc package (which
// cannot depend on focus). Contract holds when the sweep returns without
// an enumeration error: Succeeded + len(Failed) == TotalScanned.
type LeaveByTargetResult struct {
	Succeeded    int
	TotalScanned int
	Failed       []FailedLeave
}

// FailedLeave records a per-session LeaveFocus failure inside a sweep.
type FailedLeave struct {
	SessionID string
	Err       error
}

// FocusMembership records that a character is actively participating in a
// focused context. A session's FocusMemberships is mutated only through
// FocusCoordinator (invariant I-6). Each membership implies one or more
// stream subscriptions, derived by the kind's FocusKindPolicy.StreamsFor.
type FocusMembership struct {
	Kind     FocusKind
	TargetID ulid.ULID
	JoinedAt time.Time
}

// focusMutatorSentinel is an unexported type that prevents construction of
// FocusMutator from outside the internal/grpc/focus package. This enforces
// invariant I-6 (Server-Authoritative Mutation) at compile time.
type focusMutatorSentinel struct{}

// FocusMutator is the mutation callback type for Store.UpdateFocusMemberships.
// Its unexported focusMutatorSentinel field makes construction impossible
// outside the internal/grpc/focus package, enforcing invariant I-6 at
// compile time. Any code attempting to construct a FocusMutator from
// outside grpc/focus fails to compile.
type FocusMutator struct {
	_      focusMutatorSentinel // unexported type blocks external construction
	Mutate func(
		current []FocusMembership,
		presenting *FocusKey,
	) (
		next []FocusMembership,
		nextPresenting *FocusKey,
		err error,
	)
}

// NewFocusMutator creates a FocusMutator. This function is callable from
// any package, but the result can only be meaningfully constructed where
// this function is visible. The focusMutatorSentinel field is embedded at
// zero value and does not need explicit initialization.
//
// NOTE: This constructor exists in the session package so that grpc/focus
// can call it. Code outside grpc/focus SHOULD NOT call this — the
// coordinator is the only sanctioned consumer. Enforcement: lint rule +
// compile-fail documentation test.
func NewFocusMutator(fn func(
	current []FocusMembership,
	presenting *FocusKey,
) (
	next []FocusMembership,
	nextPresenting *FocusKey,
	err error,
),
) FocusMutator {
	return FocusMutator{Mutate: fn}
}

// sessionConnectionMutatorSentinel is an unexported type that prevents
// construction of SessionConnectionMutator from outside the
// internal/grpc/focus package — same compile-time enforcement as
// focusMutatorSentinel at :91-93. Phase 5 (INV-P5-7) requires that
// per-Connection focus mutations route through the Coordinator alone.
type sessionConnectionMutatorSentinel struct{}

// SessionConnectionMutator atomically mutates BOTH Info (PresentingFocus
// + future session-scoped fields) AND a single Connection (FocusKey)
// under one Store-lock acquisition. Phase 5 introduced this in place
// of the separate FocusMutator + ConnectionMutator two-call pattern
// the round-2 reviewer flagged: that pattern admitted a torn-state
// observer window between the two locked sections. This single mutator
// closes that window (INV-P5-7).
//
// FocusMutator (above) retains its existing role for FocusMemberships /
// PresentingFocus-only mutations (LeaveFocus, JoinFocus). The two
// mutator types co-exist and have non-overlapping use cases.
type SessionConnectionMutator struct { //nolint:revive // session.SessionConnectionMutator stutters by design: mirrors FocusMutator naming convention and is referenced by name in plan, ADR, and spec invariants
	_      sessionConnectionMutatorSentinel
	Mutate func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error)
}

// NewSessionConnectionMutator parallels NewFocusMutator. Callable from
// any package; the sentinel field's unexported type blocks direct struct
// literal construction outside session for the *non-keyed* form, but Go
// does allow keyed literals like `SessionConnectionMutator{Mutate: fn}`
// from any package (the unexported sentinel field stays at zero). The
// "only grpc/focus is the legitimate caller" rule is enforced via a
// lint rule + compile-fail documentation test (see
// internal/grpc/focus/session_connection_mutator_doctest.go). Defense
// against a nil Mutate panic — whether from a bypassed constructor or
// a zero-value literal — lives at the Store call sites via NilSafe (see below).
func NewSessionConnectionMutator(
	fn func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error),
) SessionConnectionMutator {
	if fn == nil {
		// Caller bug: NewSessionConnectionMutator(nil) cannot do useful
		// work and would panic later inside Store.UpdateSessionConnection.
		// Fail loudly here at construction time.
		panic("session.NewSessionConnectionMutator: nil Mutate function")
	}
	return SessionConnectionMutator{Mutate: fn}
}

// NilSafe returns ErrNilMutator if Mutate is nil (e.g., from a zero-value
// SessionConnectionMutator{} or a bypassed constructor); otherwise nil. Store
// implementations MUST call this before invoking mut.Mutate to avoid panics
// across the trust boundary. (CodeRabbit PR #4191)
func (m SessionConnectionMutator) NilSafe() error {
	if m.Mutate == nil {
		return ErrNilMutator
	}
	return nil
}

// ErrNilMutator surfaces when a SessionConnectionMutator with nil Mutate
// is passed to a Store. Sentinel for errors.Is comparison.
var ErrNilMutator = &nilMutatorError{}

type nilMutatorError struct{}

func (*nilMutatorError) Error() string {
	return "session: SessionConnectionMutator.Mutate is nil — construct via NewSessionConnectionMutator"
}

// Info contains all state for a persistent game session.
type Info struct {
	ID          string
	CharacterID ulid.ULID
	PlayerID    ulid.ULID
	// PlayerSessionID is the ULID of the PlayerSession that created this
	// game session. Used for cascade deletion: revoking or expiring a
	// PlayerSession removes all of its child game sessions atomically
	// via the sessions.player_session_id FK (ON DELETE CASCADE).
	// Zero value indicates a legacy session created before this column
	// existed.
	PlayerSessionID ulid.ULID
	CharacterName   string
	LocationID      ulid.ULID
	// LocationArrivedAt is the per-session attach floor for history queries
	// (invariant INV-PRIVACY-1). Set on login and on each location move.
	LocationArrivedAt time.Time
	// GuestCharacterCreatedAt is the guest identity overlay floor for history
	// queries (invariant INV-PRIVACY-2). Zero for non-guest sessions.
	GuestCharacterCreatedAt time.Time
	IsGuest                 bool
	Status                  Status
	GridPresent             bool
	CommandHistory          []string
	TTLSeconds              int
	MaxHistory              int
	DetachedAt              *time.Time
	ExpiresAt               *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
	LastPaged               string
	LastWhispered           string

	// FocusMemberships is the set of focused contexts this session is
	// actively participating in. Mutated only via FocusCoordinator
	// (invariant I-6). Survives disconnect; used by reconnect-resume
	// to restore the session's exact participation state.
	FocusMemberships []FocusMembership

	// PresentingFocus, if non-nil, points to the FocusMembership whose
	// stream should be foregrounded on reconnect. Used primarily by
	// telnet's single-pane reconnect UX. Web clients may update this
	// lazily or skip updating. Must either be nil or reference an
	// existing entry in FocusMemberships (invariant I-2).
	PresentingFocus *FocusKey
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
	ID         ulid.ULID
	SessionID  string
	ClientType string   // "terminal", "comms_hub", "telnet"
	Streams    []string // event streams this connection subscribes to
	// FocusKey is the per-connection focus pointer (Phase 5, INV-P5-2).
	// nil = grid focus (default for new connections); non-nil = focused
	// on the named context. Mutated only via the Coordinator-invoked
	// SessionConnectionMutator (I-6 server-authoritative; INV-P5-7).
	FocusKey    *FocusKey
	ConnectedAt time.Time
}

// LapsedConnection is the projection the lease sweep needs: enough to remove
// the row and recompute the owning session's derived liveness (holomush-rsoe6).
type LapsedConnection struct {
	ID         ulid.ULID
	SessionID  string
	ClientType string
}

// Access provides session operations for command handlers.
// This is a narrow subset of Store — only what handlers need.
type Access interface {
	// ListActive returns all sessions with status=active.
	ListActive(ctx context.Context) ([]*Info, error)

	// FindByCharacter returns the active or detached session for a character.
	FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// FindByCharacterName returns the active session for a character by name.
	// The lookup is case-insensitive.
	FindByCharacterName(ctx context.Context, name string) (*Info, error)

	// DeleteByCharacter finds and deletes a character's session.
	// Returns the deleted Info for caller use (disconnect hooks, leave events).
	// Returns nil, nil if no session exists.
	DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// UpdateActivity bumps the updated_at timestamp for a session.
	UpdateActivity(ctx context.Context, id string) error

	// UpdateLastPaged records the name of the character most recently paged.
	UpdateLastPaged(ctx context.Context, sessionID string, name string) error

	// UpdateLastWhispered records the name of the character most recently whispered to.
	UpdateLastWhispered(ctx context.Context, sessionID string, name string) error
}

// Store manages persistent session state. Implementations MUST be
// safe for concurrent use.
type Store interface {
	// Get retrieves a session by ID.
	Get(ctx context.Context, id string) (*Info, error)

	// Set creates or updates a session.
	Set(ctx context.Context, id string, info *Info) error

	// Delete removes a session.
	Delete(ctx context.Context, id string) error

	// FindByCharacter returns the active or detached session for a character.
	FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// ListByPlayer returns all non-expired sessions for a player's characters.
	// TODO: filter by playerID when player-character relationship table exists.
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*Info, error)

	// ListByPlayerSession returns all active/detached sessions whose
	// PlayerSessionID matches any of the given IDs. Returns empty slice
	// (not nil error) if playerSessionIDs is empty.
	ListByPlayerSession(ctx context.Context, playerSessionIDs []ulid.ULID) ([]*Info, error)

	// ListExpired returns all sessions past their expiry time.
	ListExpired(ctx context.Context) ([]*Info, error)

	// UpdateStatus transitions a session's status.
	UpdateStatus(ctx context.Context, id string, status Status,
		detachedAt *time.Time, expiresAt *time.Time) error

	// ReattachCAS atomically transitions a detached session to active.
	// Returns true if the row was updated, false if another client won the race.
	ReattachCAS(ctx context.Context, id string) (bool, error)

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

	// UpdateLocationOnMove atomically updates the LocationID and
	// LocationArrivedAt for all Active sessions belonging to characterID.
	// Detached and Expired sessions are not touched.
	// Called by the movement hook after a character changes location.
	//
	// arrivedAt MUST be non-zero — a zero time.Time would collapse the
	// INV-PRIVACY-1 per-session location floor to year-1 and silently disable
	// history privacy. Implementations MUST return INVALID_ARGUMENT in
	// that case.
	UpdateLocationOnMove(ctx context.Context, characterID, newLocationID ulid.ULID, arrivedAt time.Time) error

	// BumpLocationArrivedAt updates LocationArrivedAt for a single session
	// regardless of its status. Used by the reattach path to refresh the
	// floor when a player reconnects to a different location than where they
	// last detached.
	//
	// arrivedAt MUST be non-zero — same INV-PRIVACY-1 invariant as
	// UpdateLocationOnMove.
	//
	// Errors:
	//   INVALID_ARGUMENT — arrivedAt is the zero value.
	//   SESSION_NOT_FOUND — sessionID does not match any session.
	BumpLocationArrivedAt(ctx context.Context, sessionID string, arrivedAt time.Time) error

	// ListActiveByLocation returns active sessions whose LocationID matches.
	// Used for presence lists — "who is connected at this location?"
	ListActiveByLocation(ctx context.Context, locationID ulid.ULID) ([]*Info, error)

	// ListActive returns all sessions with status=active.
	ListActive(ctx context.Context) ([]*Info, error)

	// DeleteByCharacter finds and deletes a character's session.
	// Returns the deleted Info, or nil if no session exists.
	DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// UpdateActivity bumps the updated_at timestamp for a session.
	UpdateActivity(ctx context.Context, id string) error

	// FindByCharacterName returns the active session for a character by name.
	// The lookup is case-insensitive.
	FindByCharacterName(ctx context.Context, name string) (*Info, error)

	// UpdateLastPaged records the name of the character most recently paged.
	UpdateLastPaged(ctx context.Context, sessionID string, name string) error

	// UpdateLastWhispered records the name of the character most recently whispered to.
	UpdateLastWhispered(ctx context.Context, sessionID string, name string) error

	// UpdateFocusMemberships atomically applies the mutator callback under
	// a single transaction. The mutator receives the current memberships
	// and presenting focus, and returns the desired state. Monotonic cursor
	// invariants (I-5) are the caller's responsibility — this method does
	// NOT mutate cursors.
	//
	// Errors:
	//   SESSION_NOT_FOUND   — sessionID does not match.
	//   SESSION_EXPIRED     — session status is "expired".
	//   FOCUS_MUTATOR_ERROR — mutator returned an error; underlying wrapped.
	UpdateFocusMemberships(ctx context.Context, sessionID string, m FocusMutator) error

	// ListByFocus returns all non-expired sessions whose FocusMemberships
	// include the given target. Used by FocusCoordinator.LeaveFocusByTarget
	// to drive cross-session fan-out (e.g., scene-end).
	ListByFocus(ctx context.Context, target FocusKey) ([]*Info, error)

	// GetConnection looks up a single Connection by ID. The lookup is
	// O(1) (primary-key SELECT in Postgres). Returns CONNECTION_NOT_FOUND
	// if absent.
	GetConnection(ctx context.Context, connectionID ulid.ULID) (*Connection, error)

	// UpdateSessionConnection atomically runs the mutator callback
	// against the named (session, connection) pair under one Store-lock
	// acquisition. Both Info AND Connection writes commit together.
	// Postgres impl: single transaction, FOR UPDATE on sessions row FIRST
	// then session_connections row (D11 canonical lock order).
	// Returns CONNECTION_NOT_FOUND if the connection isn't registered.
	UpdateSessionConnection(
		ctx context.Context,
		sessionID string,
		connectionID ulid.ULID,
		m SessionConnectionMutator,
	) error

	// ListConnectionsBySession returns a snapshot of all active
	// Connections for a session. Used by AutoFocusOnJoin's fan-out
	// enumeration. Returns an empty slice (nil error) if the session
	// has no connections; returns SESSION_NOT_FOUND if the session
	// itself doesn't exist.
	ListConnectionsBySession(ctx context.Context, sessionID string) ([]*Connection, error)

	// RefreshConnection bumps a connection's lease (last_seen_at = now).
	// Called periodically by the gateway while the client socket is open
	// (holomush-rsoe6, I-LIVE-2). The write is scoped to sessionID so a
	// connection can only be refreshed by its owning session — a caller
	// cannot keep a foreign connection alive by pairing its ULID with a
	// session they control. Returns CONNECTION_NOT_FOUND when no row matches
	// both connectionID AND sessionID (absent or owned by another session —
	// indistinguishable, enumeration-safe, I-SEC-1).
	RefreshConnection(ctx context.Context, connectionID ulid.ULID, sessionID string) error

	// ListLapsedConnections returns connections whose lease is older than
	// olderThan (i.e. last_seen_at < olderThan). Used by the lease sweep
	// to identify stale connections for reaping (holomush-rsoe6, I-LIVE-2).
	ListLapsedConnections(ctx context.Context, olderThan time.Time) ([]LapsedConnection, error)
}
