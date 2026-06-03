// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// driveFocusDeltas computes the per-connection subscription delta from
// oldFK→newFK and delivers it to each connection via the coordinator's
// ConnectionSender. This is the single common-path driver for BOTH plugin
// runtimes (INV-SCENE-38): the binary RPC handler and the Lua hostfunc adapter
// both reach it through AutoFocusOnJoin / SetConnectionFocus.
//
// Best-effort and non-fatal (INV-SCENE-45): a delivery failure is logged but never
// fails the focus mutation, and one connection's failure does not abort the
// rest. A nil connectionSender (e.g. tests / deployments without a registry)
// skips delivery — preserving the holomush-y5inx.9 nil-skip behavior, now
// uniformly for both runtimes.
func (c *defaultCoordinator) driveFocusDeltas(
	ctx context.Context,
	sessionID string,
	charLocationID ulid.ULID,
	oldFK, newFK *session.FocusKey,
	conns []ulid.ULID,
) {
	if c.connectionSender == nil || sessionID == "" || len(conns) == 0 {
		return
	}
	gameID := c.gameID
	if gameID == "" {
		gameID = "main"
	}
	oldStreams := ComputeFocusManagedStreams(oldFK, charLocationID, gameID)
	newStreams := ComputeFocusManagedStreams(newFK, charLocationID, gameID)
	adds, removes := StreamDeltas(oldStreams, newStreams)
	for _, connID := range conns {
		for _, stream := range adds {
			if err := c.connectionSender.SendToConnection(sessionID, connID, stream, true); err != nil {
				slog.WarnContext(ctx, "focus delta add delivery failed",
					"session_id", sessionID, "connection_id", connID.String(),
					"stream", stream, "error", err)
			}
		}
		for _, stream := range removes {
			if err := c.connectionSender.SendToConnection(sessionID, connID, stream, false); err != nil {
				slog.WarnContext(ctx, "focus delta remove delivery failed",
					"session_id", sessionID, "connection_id", connID.String(),
					"stream", stream, "error", err)
			}
		}
	}
}
