// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// RestoreConnectionFocus restores a reconnecting Connection's FocusKey from
// the session's PresentingFocus, gated on FocusMemberships validation. Pins
// INV-SCENE-18 (validation under one Store-lock acquisition; grid fallback on
// revoked membership) and INV-SCENE-25 (reconnect vs concurrent LeaveFocus
// serializes via SessionConnectionMutator under the same store-side lock —
// no torn state, no read-then-mutate race).
//
// Spec §8: a single UpdateSessionConnection mutator reads PresentingFocus
// and FocusMemberships from one locked snapshot and dispatches on three
// branches:
//
//  1. PresentingFocus == nil → no-op (grid is the default for new conns).
//  2. PresentingFocus != nil AND a matching FocusMembership exists →
//     restore: conn.FocusKey = &copy(*PresentingFocus). The copy is
//     intentional: the per-connection FocusKey must not alias the
//     session-scoped PresentingFocus pointer (caller mutation through
//     either side would corrupt the other).
//  3. PresentingFocus != nil but no matching membership (revoked while
//     disconnected) → no-op + structured warning log. In-band signal to
//     the client is deferred to holomush-3d9o.
func (c *defaultCoordinator) RestoreConnectionFocus(
	ctx context.Context,
	sessionID string,
	connectionID ulid.ULID,
) error {
	m := session.NewSessionConnectionMutator(
		func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
			if info.PresentingFocus == nil {
				return info, conn, nil // grid default
			}
			pf := info.PresentingFocus
			if !hasMembership(info.FocusMemberships, pf.Kind, pf.TargetID) {
				// Membership revoked while disconnected; log warning, fall
				// back to grid. In-band UX signal deferred to holomush-3d9o.
				slog.WarnContext(
					ctx, "scene.focus.restore_fallback_to_grid",
					"session_id", sessionID,
					"character_id", info.CharacterID.String(),
					"prior_presenting_focus", pf,
				)
				return info, conn, nil
			}
			cpy := *pf
			conn.FocusKey = &cpy
			return info, conn, nil
		},
	)
	return c.sessionStore.UpdateSessionConnection(ctx, sessionID, connectionID, m) //nolint:wrapcheck // store errors are already oops-coded
}

// hasMembership reports whether memberships contains a FocusMembership
// matching the given (kind, targetID).
func hasMembership(memberships []session.FocusMembership, kind session.FocusKind, targetID ulid.ULID) bool {
	for _, m := range memberships {
		if m.Kind == kind && m.TargetID == targetID {
			return true
		}
	}
	return false
}
