// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package focuscontract is the canonical home for the focus contract types —
// the Coordinator interface and the value types its methods exchange. It is a
// dependency leaf holding contract declarations only, with no behavior: its
// entire import set is stdlib + oklog/ulid + internal/session.
//
// It exists to break a forbidden edge: internal/plugin must not import
// internal/grpc/focus. The plugin manager is the grpc tree's own consumer, so
// an upward import into a grpc subpackage inverts the layering that ARCH-02's
// decomposition depends on. Packages below internal/grpc depend on this leaf
// instead; internal/grpc/focus re-exports every declaration here as a type
// alias, so the two spellings name identical types.
package focuscontract

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// Coordinator is the sole authoritative mutator of a session's
// focused-context state.
type Coordinator interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	// LeaveFocusByTarget removes the given focus membership from every
	// non-expired session that holds it. Used for cross-session fan-out
	// (e.g., scene end). Returns a LeaveByTargetResult describing the
	// sweep; per-session failures are carried in result.Failed.
	//
	// Error semantics: the returned error covers only the enumeration
	// step (session store ListByFocus). On enumeration failure the
	// result is zero-valued and the error is coded FOCUS_SWEEP_LIST_FAILED.
	// Per-session errors live on result.Failed[].Err; callers that want
	// to retry iterate that slice. See LeaveByTargetResult for the full
	// state-space.
	LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error)
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	RestoreFocus(ctx context.Context, sessionID string) (RestorePlan, error)
	// IsAnyConnFocused reports whether any of the character's connections are
	// currently focused on the given scene. Returns (false, nil) when the
	// character has no active session (SESSION_NOT_FOUND → false per spec §6.3).
	IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error)
	// RestoreConnectionFocus restores a reconnecting Connection's FocusKey
	// from the session's PresentingFocus, gated on FocusMemberships. INV-SCENE-18
	// (validation + grid fallback under one Store-lock acquisition) +
	// INV-SCENE-25 (reconnect vs concurrent LeaveFocus serializes via the
	// SessionConnectionMutator path). See restore_connection_focus.go for
	// the three-branch decision table.
	RestoreConnectionFocus(ctx context.Context, sessionID string, connectionID ulid.ULID) error
	// SetConnectionFocus mutates a single Connection.FocusKey and
	// (D9-gated) Info.PresentingFocus atomically under one Store-lock
	// acquisition. Pins INV-SCENE-14 (FocusMemberships gate on scene targets)
	// and INV-SCENE-26 (scene grid preserves PresentingFocus). Returns
	// SetConnectionFocusResult so the coordinator itself can drive
	// per-connection subscription deltas via ComputeFocusManagedStreams +
	// StreamDeltas + SendToConnection without a second store round-trip
	// (INV-SCENE-38, see driveFocusDeltas).
	// isSceneGrid=true MUST NOT touch Info.PresentingFocus.
	SetConnectionFocus(
		ctx context.Context,
		connectionID ulid.ULID,
		focusKey *session.FocusKey,
		isSceneGrid bool,
	) (SetConnectionFocusResult, error)
	// AutoFocusOnJoin fans out a focus assignment to every terminal/telnet
	// connection belonging to characterID's active session, targeting sceneID.
	// Pins INV-SCENE-17 (terminal-only filter) and INV-SCENE-24 (D8 skip-already-focused).
	// SESSION_NOT_FOUND → empty response, nil error (consistent with T16).
	// Per-connection failures (membership_absent, connection_not_found) are
	// carried in AutoFocusOnJoinResponse.FailedConnectionIDs, not returned as
	// the error. The error return is reserved for store-level failures.
	AutoFocusOnJoin(
		ctx context.Context,
		characterID, sceneID ulid.ULID,
	) (AutoFocusOnJoinResponse, error)
	// GetConnectionFocus returns the current FocusKey for the given connection,
	// or nil when the connection is grid-focused. Returns CONNECTION_NOT_FOUND
	// when the connection does not exist; callers SHOULD treat this as absent
	// focus rather than an error (the connection may have disconnected between
	// the command dispatch and this lookup).
	GetConnectionFocus(ctx context.Context, connectionID ulid.ULID) (*session.FocusKey, error)
}

// RestorePlan is the ordered list of streams and their replay modes to
// apply when a Subscribe handler starts.
type RestorePlan struct {
	Streams          []StreamWithMode
	PresentingStream string // empty if no presenting focus
}

// SetConnectionFocusResult carries the outputs of a SetConnectionFocus call
// consumed by focus.Coordinator.driveFocusDeltas to compute per-Connection
// subscription deltas without a second store round-trip (INV-SCENE-38).
type SetConnectionFocusResult struct {
	// OldFocusKey is the Connection.FocusKey value captured before the mutation.
	// Nil means the connection was on the grid (no prior explicit focus).
	OldFocusKey *session.FocusKey
	// SessionID is the session that owns the mutated connection. Used by
	// SendToConnection to route the subscription update to the right goroutine.
	SessionID string
	// CharLocationID is the session's LocationID at mutation time, used to
	// compute grid stream names (location:<charLocationID>) for subscription
	// delta routing.
	CharLocationID ulid.ULID
}

// AutoFocusOnJoinResponse carries the fan-out result from AutoFocusOnJoin.
// The binary plugin host translates this to the wire format
// (host.v1 AutoFocusOnJoinResponse).
type AutoFocusOnJoinResponse struct {
	// SessionID is the session that owns the auto-focused connections. Consumed
	// by focus.Coordinator.driveFocusDeltas to route SendToConnection calls
	// without a second store round-trip (INV-SCENE-38). Empty when SESSION_NOT_FOUND
	// (no active session).
	SessionID string
	// CharLocationID is the session's LocationID at mutation time. Consumed by
	// focus.Coordinator.driveFocusDeltas to compute grid stream names for
	// subscription delta routing (location:<charLocationID> for grid-focused
	// connections).
	CharLocationID ulid.ULID
	// FocusedConnectionIDs are connections that were successfully auto-focused.
	FocusedConnectionIDs []ulid.ULID
	// SkippedConnectionIDs are connections that were already explicitly focused
	// on a different target (INV-SCENE-24, D8 skip-rule).
	SkippedConnectionIDs []ulid.ULID
	// FailedConnectionIDs are connections that could not be focused, with reason.
	FailedConnectionIDs []AutoFocusFailure
	// TotalConnectionCount is the count of ALL connections on the session,
	// regardless of client type filter. Used for diagnostic counters.
	TotalConnectionCount uint32
}

// AutoFocusFailure describes a per-connection failure during AutoFocusOnJoin.
type AutoFocusFailure struct {
	// ConnectionID is the connection that could not be focused.
	ConnectionID ulid.ULID
	// Reason is one of "membership_absent" or "connection_not_found".
	Reason string
}

// ReplayMode is an alias for session.ReplayMode. Defined here for
// backward compatibility with existing focus package consumers.
// New code SHOULD import session.ReplayMode directly.
type ReplayMode = session.ReplayMode

// Re-export constants so existing focus.ReplayModeXxx references compile.
const (
	ReplayModeFromCursor  = session.ReplayModeFromCursor
	ReplayModeBoundedTail = session.ReplayModeBoundedTail
	ReplayModeLiveOnly    = session.ReplayModeLiveOnly
)

// StreamWithMode pairs a stream name with its replay mode and optional
// mode-specific parameters.
type StreamWithMode struct {
	Stream    string
	Mode      ReplayMode
	TailCount int       // for ReplayModeBoundedTail
	NotBefore time.Time // for ReplayModeBoundedTail
}
