// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"strings"
	"time"

	"github.com/holomush/holomush/internal/access"
	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/session"
)

// maxStreamScopeFloor returns MAX(streamScopeFloor(info, subj)) across every
// filter subject — the per-session aggregate floor used at OpenSession entry.
// Per holomush-iwzt §6.2 Tier 1: MAX (not MIN) yields the smallest set that
// includes events visible to at least one subject; iwzt.15 then drops events
// below the per-subject floor at delivery time.
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
	case isSceneStream(stream):
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
	case isCharacterStream(stream):
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

// isLocationStream reports whether a stream is a qualified location subject:
// events.<gameID>.location.<ULID> (exactly 4 segments). Dot-only per
// holomush-rops / holomush-iwzt §3; the legacy location:<ulid> colon form is
// no longer accepted.
func isLocationStream(stream string) bool {
	parts := strings.Split(stream, ".")
	return len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "location" && parts[3] != ""
}

// extractLocationID returns the ULID portion of a qualified location stream
// (events.<gameID>.location.<ULID>). Caller MUST check isLocationStream first;
// otherwise behavior is undefined.
func extractLocationID(stream string) string {
	parts := strings.Split(stream, ".")
	if len(parts) == 4 {
		return parts[3]
	}
	return ""
}

// staffOverride reports whether the session's character has been granted the
// read_unrestricted_history action via ABAC (INV-PRIVACY-6 / ADR wxty). When true,
// the location hard-gate (INV-PRIVACY-1) is bypassed; the temporal floor still
// applies. Returns false if the engine is nil or if evaluation fails
// (fail-closed).
func staffOverride(ctx context.Context, info *session.Info, engine accessTypes.AccessPolicyEngine) bool {
	if engine == nil {
		return false
	}
	accessReq, err := accessTypes.NewAccessRequest(
		access.CharacterSubject(info.CharacterID.String()),
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
