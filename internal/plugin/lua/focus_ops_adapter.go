// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
)

// coordinatorFocusOpsAdapter adapts a focus.Coordinator to the hostfunc.FocusOps interface.
// All Phase 5 methods (SetConnectionFocus, AutoFocusOnJoin, IsAnyConnFocused)
// delegate to the wrapped coordinator. AutoFocusOnJoin translates the
// AutoFocusOnJoinResponse struct to the FocusOps tuple shape
// (focused, skipped []ulid.ULID, failed []FocusFailure, totalConnCount uint32, err error).
// This adapter is the permanent Lua delegation seam: it lets the gopher-lua
// hostfunc layer reach the same focus.Coordinator the binary host uses, so both
// runtimes drive focus deltas through one common path (INV-SCENE-38).
type coordinatorFocusOpsAdapter struct {
	c focus.Coordinator
}

var _ hostfunc.FocusOps = (*coordinatorFocusOpsAdapter)(nil)

func (a *coordinatorFocusOpsAdapter) JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.c.JoinFocus(ctx, sessionID, target) //nolint:wrapcheck // coordinator errors are already oops-coded
}

func (a *coordinatorFocusOpsAdapter) LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.c.LeaveFocus(ctx, sessionID, target) //nolint:wrapcheck // coordinator errors are already oops-coded
}

func (a *coordinatorFocusOpsAdapter) LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error) {
	return a.c.LeaveFocusByTarget(ctx, target) //nolint:wrapcheck // coordinator errors are already oops-coded
}

func (a *coordinatorFocusOpsAdapter) PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.c.PresentFocus(ctx, sessionID, target) //nolint:wrapcheck // coordinator errors are already oops-coded
}

// SetConnectionFocus delegates to the coordinator. The FocusOps surface
// returns only error; the coordinator's oldFocusKey return is consumed inside
// focus.Coordinator.driveFocusDeltas (which the coordinator calls itself), so
// dropping it here is safe. Per-connection subscription deltas are driven
// inside focus.Coordinator (INV-SCENE-38), so the adapter needs only to
// delegate; the dropped oldFocusKey return value is not needed here.
func (a *coordinatorFocusOpsAdapter) SetConnectionFocus(ctx context.Context, connectionID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) error {
	_, err := a.c.SetConnectionFocus(ctx, connectionID, focusKey, isSceneGrid)
	return err //nolint:wrapcheck // coordinator errors are already oops-coded
}

// AutoFocusOnJoin delegates to the coordinator and translates the
// AutoFocusOnJoinResponse struct to the FocusOps tuple shape — the
// individual slices and total count — discarding fields (SessionID,
// CharLocationID) that the coordinator consumes internally in
// driveFocusDeltas and the Lua tuple surface does not need.
func (a *coordinatorFocusOpsAdapter) AutoFocusOnJoin(ctx context.Context, characterID, sceneID ulid.ULID) (focused, skipped []ulid.ULID, failed []hostfunc.FocusFailure, totalConnCount uint32, err error) {
	resp, err := a.c.AutoFocusOnJoin(ctx, characterID, sceneID)
	if err != nil {
		return nil, nil, nil, 0, err //nolint:wrapcheck // coordinator errors are already oops-coded
	}
	// Translate AutoFocusFailure slice to hostfunc.FocusFailure slice.
	var hfFailed []hostfunc.FocusFailure
	if len(resp.FailedConnectionIDs) > 0 {
		hfFailed = make([]hostfunc.FocusFailure, len(resp.FailedConnectionIDs))
		for i, f := range resp.FailedConnectionIDs {
			hfFailed[i] = hostfunc.FocusFailure{
				ConnectionID: f.ConnectionID,
				Reason:       f.Reason,
			}
		}
	}
	return resp.FocusedConnectionIDs, resp.SkippedConnectionIDs, hfFailed, resp.TotalConnectionCount, nil
}

func (a *coordinatorFocusOpsAdapter) IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error) {
	return a.c.IsAnyConnFocused(ctx, characterID, sceneID) //nolint:wrapcheck // coordinator errors are already oops-coded
}
