// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// GetConnectionFocus returns the current FocusKey for the given connection, or
// nil when the connection is grid-focused (FocusKey is nil). Returns
// CONNECTION_NOT_FOUND when the connection does not exist; callers SHOULD treat
// this as absent focus — the connection may have disconnected between dispatch
// and this lookup.
func (c *defaultCoordinator) GetConnectionFocus(
	ctx context.Context,
	connectionID ulid.ULID,
) (*session.FocusKey, error) {
	conn, err := c.sessionStore.GetConnection(ctx, connectionID)
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "CONNECTION_NOT_FOUND" {
			slog.DebugContext(ctx, "focus.GetConnectionFocus: connection not found; treating as absent focus",
				"connection_id", connectionID.String())
			return nil, nil
		}
		return nil, err //nolint:wrapcheck // store errors are already oops-coded
	}
	return conn.FocusKey, nil
}
