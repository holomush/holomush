// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"strings"
	"time"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/session"
)

// maxStreamScopeFloor returns MAX(streamScopeFloor(info, subj)) across every
// filter subject — the per-session aggregate floor used at OpenSession entry.
// Per holomush-iwzt §6.2 Tier 1: MAX (not MIN) yields the smallest set that
// includes events visible to at least one subject; iwzt.15 then drops events
// below the per-subject floor at delivery time.
//
// NOTE: streamScopeFloor currently inspects legacy stream-name prefixes
// ("location:", "scene:"), but production callers pass NATS subjects
// ("events.<gid>.location.X"), so the loop returns time.Time{} for every
// real-world subject today. Until that format mismatch is closed (tracked
// as a separate follow-up bead), the aggregate floor is effectively zero —
// preserving the pre-iwzt DeliverAllPolicy behavior. The helper exists so
// the MAX-semantics are testable in isolation and the wiring is in place
// when the format gap is closed.
func maxStreamScopeFloor(info *session.Info, filters []string) time.Time {
	var minFloor time.Time
	for _, subj := range filters {
		f := streamScopeFloor(info, subj)
		if f.After(minFloor) {
			minFloor = f
		}
	}
	return minFloor
}

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
func extractLocationID(stream string) string {
	return strings.TrimPrefix(stream, "location:")
}

// staffOverride reports whether the session's character has been granted the
// read_unrestricted_history action via ABAC (I-PRIV-6 / ADR wxty). When true,
// the location hard-gate (I-PRIV-1) is bypassed; the temporal floor still
// applies. Returns false if the engine is nil or if evaluation fails
// (fail-closed).
func staffOverride(ctx context.Context, info *session.Info, engine accessTypes.AccessPolicyEngine) bool {
	if engine == nil {
		return false
	}
	accessReq, err := accessTypes.NewAccessRequest(
		"character:"+info.CharacterID.String(),
		"read_unrestricted_history",
		"stream:*",
		nil,
	)
	if err != nil {
		return false
	}
	decision, evalErr := engine.Evaluate(ctx, accessReq)
	return evalErr == nil && decision.IsAllowed()
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
