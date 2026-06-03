// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// SetConnectionFocus is the Coordinator-side implementation of the Phase 5
// SetConnectionFocus RPC. It delegates to a single
// Store.UpdateSessionConnection call so Connection.FocusKey and (D9-gated)
// Info.PresentingFocus commit atomically under one Store-lock acquisition
// (D7, INV-SCENE-20).
//
// Invariants pinned here:
//
//   - INV-SCENE-14 (FocusMemberships gate): a non-nil scene-kind focusKey is
//     rejected with FOCUS_WITHOUT_MEMBERSHIP unless info.FocusMemberships
//     contains the matching {kind, target_id}. Membership validation runs
//     inside the same store-locked snapshot the write commits against — no
//     read-then-mutate TOCTOU window.
//
//   - INV-SCENE-26 (scene grid preserves PresentingFocus): when isSceneGrid is
//     true, Info.PresentingFocus is NOT touched, even if focusKey is nil.
//     This ensures a scene-grid pivot from terminal does not destroy the
//     session's last explicit focus — reconnect must land on the explicit
//     focus, not the grid.
//
//   - D9 (terminal/telnet explicit focus): only terminal/telnet client
//     types update Info.PresentingFocus, and only when isSceneGrid is
//     false (i.e., explicit focus change). comms_hub focus changes write
//     Connection.FocusKey but leave PresentingFocus alone (D9 split-brain
//     guard).
//
// Returns SetConnectionFocusResult so the coordinator can drive stream deltas
// via ComputeFocusManagedStreams + StreamDeltas + SendToConnection without a
// second store round-trip (INV-SCENE-38, see driveFocusDeltas). The pre-mutation FocusKey is captured
// inside the mutator closure via an outer-variable binding; on any error
// path OldFocusKey returns nil so partial state cannot leak.
func (c *defaultCoordinator) SetConnectionFocus(
	ctx context.Context,
	connectionID ulid.ULID,
	focusKey *session.FocusKey,
	isSceneGrid bool,
) (SetConnectionFocusResult, error) {
	// Look up the connection to learn its session_id — the RPC carries only
	// connection_id, but Store.UpdateSessionConnection is keyed by session.
	conn, err := c.sessionStore.GetConnection(ctx, connectionID)
	if err != nil {
		return SetConnectionFocusResult{}, err //nolint:wrapcheck // store errors are already oops-coded (CONNECTION_NOT_FOUND)
	}
	sessionID := conn.SessionID
	isTerminal := conn.ClientType == "terminal" || conn.ClientType == "telnet"

	var result SetConnectionFocusResult
	result.SessionID = sessionID

	m := session.NewSessionConnectionMutator(
		func(si session.Info, sc session.Connection) (session.Info, session.Connection, error) {
			// Capture LocationID from the LOCKED snapshot (not from a pre-lock
			// read), so the subscription delta computed downstream uses the
			// character's location as observed inside the same critical section
			// as the focus mutation. A concurrent location move otherwise leaks
			// a stale ID into ComputeFocusManagedStreams. (CodeRabbit PR #4191)
			result.CharLocationID = si.LocationID
			// INV-SCENE-14: scene focus requires a matching FocusMembership.
			if focusKey != nil && focusKey.Kind == session.FocusKindScene {
				if !hasMembership(si.FocusMemberships, focusKey.Kind, focusKey.TargetID) {
					return si, sc, oops.Code("FOCUS_WITHOUT_MEMBERSHIP").
						With("character_id", si.CharacterID.String()).
						With("scene_id", focusKey.TargetID.String()).
						Errorf("focus target not in session FocusMemberships")
				}
			}

			// Capture pre-mutation conn.FocusKey for the post-commit
			// subscription delta (driveFocusDeltas). Copy by value to break
			// aliasing with the (about-to-be-replaced) per-conn pointer.
			if sc.FocusKey != nil {
				cpy := *sc.FocusKey
				result.OldFocusKey = &cpy
			}

			// Write Connection.FocusKey unconditionally (nil clears to grid).
			sc.FocusKey = focusKey

			// D9 + INV-SCENE-26: only terminal/telnet explicit focus changes
			// update PresentingFocus. Scene-grid pivots (isSceneGrid=true)
			// MUST leave it alone so reconnect lands on the prior explicit
			// focus, not the grid.
			if isTerminal && !isSceneGrid {
				si.PresentingFocus = focusKey
			}
			return si, sc, nil
		},
	)

	if uerr := c.sessionStore.UpdateSessionConnection(ctx, sessionID, connectionID, m); uerr != nil {
		// On error, no state committed to the store. Membership-validation
		// returns BEFORE the outer-variable capture runs, so OldFocusKey
		// stays nil; returning zero result makes the contract observable
		// even if a future mutator reorders captures above the error return.
		return SetConnectionFocusResult{}, uerr //nolint:wrapcheck // store errors are already oops-coded
	}
	// INV-SCENE-38: drive the per-connection subscription delta at the common path.
	// Old streams derive from the pre-mutation FocusKey (result.OldFocusKey;
	// nil = grid), new streams from focusKey (the requested target; nil = grid).
	c.driveFocusDeltas(ctx, result.SessionID, result.CharLocationID, result.OldFocusKey, focusKey, []ulid.ULID{connectionID})

	return result, nil
}
