// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"strings"
	"time"

	"github.com/holomush/holomush/internal/session"
)

// streamScopeFloor returns the temporal floor for a session's view of the
// given stream. Per holomush-iwzt §6.1.
func streamScopeFloor(info *session.Info, stream string) time.Time {
	var base time.Time
	switch {
	case isLocationStream(stream):
		base = info.LocationArrivedAt
	case strings.HasPrefix(stream, "scene:"):
		sceneID, ok := extractSceneID(stream)
		if !ok {
			return time.Time{}
		}
		for _, m := range info.FocusMemberships {
			if m.Kind == session.FocusKindScene && m.TargetID.String() == sceneID {
				base = m.JoinedAt
				break
			}
		}
	case strings.HasPrefix(stream, "character:"):
		return time.Time{}
	default:
		return time.Time{}
	}
	// Guest identity overlay: when GuestCharacterCreatedAt is non-zero (set
	// at session creation for guest players), apply it as the floor if it's
	// later than the base. Use the non-zero timestamp as the guest signal
	// rather than session.Info.IsGuest — the IsGuest flag is also read at
	// `internal/grpc/server.go::Disconnect` to trigger immediate session
	// deletion, which breaks page-reload reattach. Tracked as a separate
	// follow-up to redesign that disconnect path.
	if !info.GuestCharacterCreatedAt.IsZero() && info.GuestCharacterCreatedAt.After(base) {
		return info.GuestCharacterCreatedAt
	}
	return base
}

// isLocationStream reports whether a stream subject is a grid location stream.
// Per holomush-iwzt §3.
func isLocationStream(stream string) bool {
	if !strings.HasPrefix(stream, "location:") {
		return false
	}
	rest := strings.TrimPrefix(stream, "location:")
	return rest != "" && !strings.Contains(rest, ":")
}

// extractLocationID returns the ULID portion of a location stream.
// Caller MUST check isLocationStream first; otherwise behavior is undefined.
func extractLocationID(stream string) string { //nolint:unused // consumed by QueryStreamHistory in iwzt.8
	return strings.TrimPrefix(stream, "location:")
}

// extractSceneID returns the scene ULID from a scene:<id>:ic or scene:<id>:ooc subject.
func extractSceneID(stream string) (string, bool) {
	rest := strings.TrimPrefix(stream, "scene:")
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	return parts[0], true
}
