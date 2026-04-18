// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// LeaveFocusByTarget sweeps all non-expired sessions whose FocusMemberships
// include the given target and calls LeaveFocus per-session. See
// session.LeaveByTargetResult for the return-value contract: enumeration
// errors are returned as the error; per-session errors are carried in
// result.Failed.
//
// Idempotent races: LeaveFocus is already idempotent for non-members —
// if a session's membership was cleared by another caller between
// ListByFocus and LeaveFocus, the mutator simply returns the existing
// list and LeaveFocus returns nil. Those sessions correctly count as
// Succeeded without any special handling here.
//
// Use case: scene end. After the DB transition commits, the host calls
// LeaveFocusByTarget to clear FocusMemberships across every participant's
// session (owner and non-owners alike). The sweep is best-effort: DB
// state is authoritative, and focus-side failures do not roll back the
// scene end.
func (c *defaultCoordinator) LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error) {
	// Reject unregistered kinds up front. Without this, unregistered kinds
	// silently return (zero-result, nil) whenever no rows match — which is
	// indistinguishable from a legitimate empty sweep and is especially
	// misleading through the Lua hostfunc path where kind is an
	// unvalidated string. JoinFocus applies the same policyFor check.
	if _, err := c.policyFor(target.Kind); err != nil {
		return session.LeaveByTargetResult{}, oops.
			With("focus_kind", string(target.Kind)).
			With("target_id", target.TargetID.String()).
			With("operation", "leave focus by target").
			Wrap(err)
	}

	sessions, err := c.sessionStore.ListByFocus(ctx, target)
	if err != nil {
		return session.LeaveByTargetResult{}, oops.Code("FOCUS_SWEEP_LIST_FAILED").
			With("focus_kind", string(target.Kind)).
			With("target_id", target.TargetID.String()).
			With("operation", "list by focus").
			Wrap(err)
	}

	result := session.LeaveByTargetResult{TotalScanned: len(sessions)}
	for _, info := range sessions {
		if leaveErr := c.LeaveFocus(ctx, info.ID, target); leaveErr != nil {
			result.Failed = append(result.Failed, session.FailedLeave{
				SessionID: info.ID,
				Err:       oops.With("session_id", info.ID).Wrap(leaveErr),
			})
			continue
		}
		result.Succeeded++
	}
	return result, nil
}
