// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// AutoFocusOnJoinResponse carries the fan-out result from AutoFocusOnJoin.
// The binary plugin host translates this to the wire format
// (PluginHostServiceAutoFocusOnJoinResponse).
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

// isTerminalLike reports whether the given clientType should participate in
// the AutoFocusOnJoin fan-out (INV-SCENE-17: terminal/telnet only).
func isTerminalLike(clientType string) bool {
	return clientType == "terminal" || clientType == "telnet"
}

// AutoFocusOnJoin fans out a focus assignment to every terminal/telnet
// connection belonging to characterID's active session, targeting sceneID.
//
// Algorithm (spec §6.2):
//  1. Resolve the character's active session via FindByCharacter. SESSION_NOT_FOUND
//     → return empty AutoFocusOnJoinResponse (consistent with IsAnyConnFocused T16).
//  2. List all connections for the session. Record TotalConnectionCount = len(all conns).
//  3. Filter to {terminal, telnet} client types (INV-SCENE-17).
//  4. For each filtered connection, call UpdateSessionConnection under one
//     Store-lock acquisition. The mutator applies:
//     - D8 skip-rule (INV-SCENE-24): conn.FocusKey != nil && *FocusKey != target →
//     return unchanged + record in SkippedConnectionIDs.
//     - INV-SCENE-14 membership gate: FocusMemberships lacks target → return
//     FOCUS_WITHOUT_MEMBERSHIP error → record in FailedConnectionIDs with
//     reason "membership_absent".
//     - Apply: conn.FocusKey = &target; terminal→ info.PresentingFocus = &target (D9);
//     record in FocusedConnectionIDs.
//  5. CONNECTION_NOT_FOUND from UpdateSessionConnection (rare race): record in
//     FailedConnectionIDs with reason "connection_not_found".
func (c *defaultCoordinator) AutoFocusOnJoin(
	ctx context.Context,
	characterID, sceneID ulid.ULID,
) (AutoFocusOnJoinResponse, error) {
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	// Step 1: resolve session. SESSION_NOT_FOUND → empty response (no error).
	info, err := c.sessionStore.FindByCharacter(ctx, characterID)
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SESSION_NOT_FOUND" {
			return AutoFocusOnJoinResponse{}, nil
		}
		return AutoFocusOnJoinResponse{}, err //nolint:wrapcheck // store errors are already oops-coded
	}

	// Step 2: list all connections and set total count.
	conns, err := c.sessionStore.ListConnectionsBySession(ctx, info.ID)
	if err != nil {
		return AutoFocusOnJoinResponse{}, err //nolint:wrapcheck // store errors are already oops-coded
	}

	resp := AutoFocusOnJoinResponse{
		SessionID:            info.ID,
		CharLocationID:       info.LocationID,
		TotalConnectionCount: uint32(len(conns)), //nolint:gosec // conn count is bounded by active connections per session
	}

	// Connections transitioning grid→scene this call — the only ones that need
	// a subscription delta. A connection already focused on this scene re-enters
	// "focused" but its streams are unchanged, so it is deliberately excluded
	// (see the driveFocusDeltas call below).
	var deltaConns []ulid.ULID

	// Step 3 + 4: filter and mutate each terminal/telnet connection.
	for _, conn := range conns {
		if !isTerminalLike(conn.ClientType) {
			continue // INV-SCENE-17: skip non-terminal/telnet connections
		}

		// Capture conn.ID for closure (loop variable safety in older Go).
		connID := conn.ID

		// Track which bucket this connection ends in, set inside the mutator.
		var outcome string // "focused" | "skipped" | "membership_absent"
		// Whether this connection was on the grid (no FocusKey) before the
		// mutation. Only grid→scene transitions need a subscription delta;
		// a connection already focused on this scene keeps its streams.
		var wasOnGrid bool

		m := session.NewSessionConnectionMutator(
			func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
				// D8 skip-rule (INV-SCENE-24): conn is already explicitly focused
				// on a DIFFERENT target — do not override.
				if conn.FocusKey != nil && *conn.FocusKey != target {
					outcome = "skipped"
					return info, conn, nil // no-op; UpdateSessionConnection commits this unchanged
				}
				// Reaching here, conn.FocusKey is either nil (grid) or already
				// == target (re-focus on the same scene). Capture it before we
				// overwrite, so the delta step can skip the no-op re-focus.
				wasOnGrid = conn.FocusKey == nil

				// INV-SCENE-14: scene focus requires a matching FocusMembership.
				if !hasMembership(info.FocusMemberships, target.Kind, target.TargetID) {
					outcome = "membership_absent"
					return info, conn, oops.Code("FOCUS_WITHOUT_MEMBERSHIP").
						With("character_id", info.CharacterID.String()).
						With("scene_id", sceneID.String()).
						Errorf("auto-focus target not in session FocusMemberships")
				}

				// Apply: write FocusKey; D9 — terminal/telnet also updates PresentingFocus.
				tgt := target // copy to prevent aliasing
				conn.FocusKey = &tgt
				if isTerminalLike(conn.ClientType) {
					pf := target // separate copy for PresentingFocus
					info.PresentingFocus = &pf
				}

				outcome = "focused"
				return info, conn, nil
			},
		)

		updateErr := c.sessionStore.UpdateSessionConnection(ctx, info.ID, connID, m)
		if updateErr != nil {
			var oe oops.OopsError
			if errors.As(updateErr, &oe) {
				switch oe.Code() {
				case "FOCUS_WITHOUT_MEMBERSHIP":
					// Membership-absent: record per-conn failure, continue fan-out.
					resp.FailedConnectionIDs = append(resp.FailedConnectionIDs, AutoFocusFailure{
						ConnectionID: connID,
						Reason:       "membership_absent",
					})
					continue
				case "CONNECTION_NOT_FOUND":
					// Race: connection disappeared between list and update.
					resp.FailedConnectionIDs = append(resp.FailedConnectionIDs, AutoFocusFailure{
						ConnectionID: connID,
						Reason:       "connection_not_found",
					})
					continue
				}
			}
			// Unexpected error — propagate.
			return AutoFocusOnJoinResponse{}, updateErr //nolint:wrapcheck // store errors are already oops-coded
		}

		// No error: classify by outcome set in the mutator.
		switch outcome {
		case "focused":
			resp.FocusedConnectionIDs = append(resp.FocusedConnectionIDs, connID)
			if wasOnGrid {
				// Only a genuine grid→scene transition needs a delta; a
				// re-focus on the same scene leaves the stream set unchanged.
				deltaConns = append(deltaConns, connID)
			}
		case "skipped":
			resp.SkippedConnectionIDs = append(resp.SkippedConnectionIDs, connID)
		}
		// outcome == "membership_absent" is handled in the error path above.
	}

	// INV-SCENE-38: drive per-connection subscription deltas at the common path.
	// deltaConns holds only connections that actually moved grid→scene this
	// call, so passing nil as the old FocusKey (grid) is correct for every one
	// of them: each gets the add(scene)+remove(location) delta exactly once.
	// A connection already focused on this SAME scene is excluded — its stream
	// set is unchanged, and re-driving a grid→scene delta would otherwise issue
	// a redundant scene re-add (which carries ReplayModeFromCursor and could
	// replay scene history again) plus a remove of a location stream it no
	// longer holds (holomush-fqv8z review finding).
	sceneFk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	c.driveFocusDeltas(ctx, resp.SessionID, resp.CharLocationID, nil, sceneFk, deltaConns)

	return resp, nil
}
