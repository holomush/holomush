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

// IsAnyConnFocused reports whether any of the character's connections are
// currently focused on the given scene. Returns (false, nil) when the
// character has no active session — spec §6.3: inactive-character emit
// loops short-circuit cleanly (CRIT-2 fix from plan-review r1).
func (c *defaultCoordinator) IsAnyConnFocused(
	ctx context.Context,
	characterID, sceneID ulid.ULID,
) (bool, error) {
	info, err := c.sessionStore.FindByCharacter(ctx, characterID)
	if err != nil {
		// "Character has no active session" surfaces as SESSION_NOT_FOUND
		// from both MemStore (memstore.go:82-84) and Postgres
		// (session_store.go:347-349). Translate to (false, nil) so the
		// plugin's notification-emission decision short-circuits cleanly
		// (spec §6.3: "if false → emit a notification").
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SESSION_NOT_FOUND" {
			return false, nil
		}
		return false, err //nolint:wrapcheck // store errors are already oops-coded
	}
	conns, err := c.sessionStore.ListConnectionsBySession(ctx, info.ID)
	if err != nil {
		return false, err //nolint:wrapcheck // store errors are already oops-coded
	}
	for _, conn := range conns {
		if conn.FocusKey != nil && conn.FocusKey.Kind == session.FocusKindScene && conn.FocusKey.TargetID == sceneID {
			return true, nil
		}
	}
	return false, nil
}
