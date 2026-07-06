// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// FocusReader reads a connection's current focus kind for redirect routing.
// Returns the zero value FocusKind("") for grid/no-focus. Implementations MUST
// treat a connection that vanished between dispatch and lookup as absent focus
// (empty kind, nil error) — that is a genuine no-focus state, not an infra
// failure. Genuine read failures MUST be returned as errors, never mapped to
// absent focus: the dispatcher fails CLOSED on them (aborts dispatch,
// holomush-uprtc) because routing to the plaintext location on a read error
// would leak a scene-focused player's participant-only content.
type FocusReader interface {
	ConnectionFocusKind(ctx context.Context, connectionID ulid.ULID) (session.FocusKind, error)
}

// connectionGetter is the narrow session-store surface the store-backed
// FocusReader needs. session.Store satisfies it.
type connectionGetter interface {
	GetConnection(ctx context.Context, connectionID ulid.ULID) (*session.Connection, error)
}

type storeFocusReader struct{ store connectionGetter }

// NewStoreFocusReader adapts a session store's GetConnection into a FocusReader.
func NewStoreFocusReader(store connectionGetter) FocusReader {
	return &storeFocusReader{store: store}
}

func (r *storeFocusReader) ConnectionFocusKind(
	ctx context.Context, connectionID ulid.ULID,
) (session.FocusKind, error) {
	conn, err := r.store.GetConnection(ctx, connectionID)
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "CONNECTION_NOT_FOUND" {
			// Connection gone between dispatch and lookup → absent focus.
			return "", nil
		}
		return "", err //nolint:wrapcheck // store errors are already oops-coded
	}
	if conn.FocusKey == nil {
		return "", nil // grid focus
	}
	return conn.FocusKey.Kind, nil
}
