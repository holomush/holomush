// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ListFocusPresence returns the set of Active sessions at the calling
// session's location. See docs/superpowers/specs/2026-05-19-presence-snapshot-design.md
// for the full design.
//
// Enumeration safety mirrors ListSessionStreams (internal/grpc/list_session_streams.go):
// every ownership-validation failure collapses to SESSION_NOT_FOUND.
//
// This is the T5 skeleton: subsequent beads (5b2j.9 / T6, 5b2j.10 / T7) add
// the expired/empty-location/focus dispatch + ABAC gate + store query + response.
func (h *QueryHandler) ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	if req == nil {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("request is required")
	}
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}
	slog.DebugContext(ctx, "list focus presence",
		"request_id", requestID,
		"session_id", req.SessionId)

	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}

	// Validate ownership — enumeration-safe collapse to SESSION_NOT_FOUND.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		h.playerSessionRepo,
		h.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "list focus presence ownership validation failed",
			"request_id", requestID, "session_id", req.SessionId, "error", err)
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", req.SessionId).
			Errorf("session not found")
	}

	// Re-Get the session info (ValidateSessionOwnership returned the player
	// session, not the user session). Mirrors ListSessionStreams pattern.
	info, err := h.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_NOT_FOUND").
				With("session_id", req.SessionId).Errorf("session not found")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").
			With("session_id", req.SessionId).Errorf("session expired")
	}

	// Scene-focused sessions are out of 5b2j scope (spec D-2). Return
	// UNIMPLEMENTED so the gap is loud, not silently degraded to a
	// location-list fallback.
	if len(info.FocusMemberships) > 0 {
		return nil, oops.Code("UNIMPLEMENTED").
			With("session_id", req.SessionId).
			With("focus_memberships", len(info.FocusMemberships)).
			Errorf("scene-focused presence not yet implemented")
	}

	// Session has no location yet (e.g., between create and SelectCharacter).
	// Not an error — return an empty list under LOCATION context.
	if info.LocationID.IsZero() {
		return &corev1.ListFocusPresenceResponse{
			Meta:    responseMeta(requestID),
			Context: corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION,
			Entries: []*corev1.PresenceEntry{},
		}, nil
	}

	// Guard optional deps against nil. Both fields are option-injected on
	// CoreServer (WithAccessEngine / WithCharacterNameResolver); production
	// wire-up at cmd/holomush/sub_grpc.go always sets them, but tests and
	// future construction sites might not. Fail-closed for the ABAC engine
	// (missing engine = default-deny); INTERNAL for the resolver (it's a
	// misconfiguration, not a security boundary).
	if h.accessEngine == nil {
		slog.ErrorContext(ctx, "list focus presence: access engine not configured",
			"request_id", requestID, "session_id", req.SessionId)
		return nil, oops.Code("PERMISSION_DENIED").
			With("session_id", req.SessionId).
			With("reason", "access_engine_not_configured").
			Errorf("permission denied")
	}
	if h.characterNameResolver == nil {
		slog.ErrorContext(ctx, "list focus presence: name resolver not configured",
			"request_id", requestID, "session_id", req.SessionId)
		return nil, oops.Code("INTERNAL").Errorf("character name resolver not configured")
	}

	// ABAC gate (INV-PRESENCE-4). Default-deny; same-location subjects allowed via
	// seed:player-location-list-presence; admin via the admin super-rule.
	accessReq, err := types.NewAccessRequest(
		access.CharacterSubject(info.CharacterID.String()),
		"list_presence",
		access.LocationResource(info.LocationID.String()),
		nil, // no extra context attributes
	)
	if err != nil {
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	decision, err := h.accessEngine.Evaluate(ctx, accessReq)
	if err != nil {
		slog.ErrorContext(ctx, "list focus presence ABAC error",
			"request_id", requestID, "error", err)
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if !decision.IsAllowed() {
		return nil, oops.Code("PERMISSION_DENIED").
			With("session_id", req.SessionId).
			With("action", "list_presence").
			With("resource", info.LocationID.String()).
			Errorf("permission denied")
	}

	// Fetch active sessions at the location.
	sessions, err := h.sessionStore.ListActiveByLocation(ctx, info.LocationID)
	if err != nil {
		slog.ErrorContext(ctx, "list active by location failed",
			"request_id", requestID, "location_id", info.LocationID.String(), "error", err)
		return nil, oops.Code("INTERNAL").Wrap(err)
	}

	// Build the unique character-ID set (INV-PRESENCE-9 dedup defense).
	seen := make(map[ulid.ULID]struct{}, len(sessions))
	uniqueIDs := make([]ulid.ULID, 0, len(sessions))
	for _, sess := range sessions {
		if _, dup := seen[sess.CharacterID]; dup {
			continue
		}
		seen[sess.CharacterID] = struct{}{}
		uniqueIDs = append(uniqueIDs, sess.CharacterID)
	}

	// Batch character-name resolution.
	names, err := h.characterNameResolver.Names(ctx, uniqueIDs)
	if err != nil {
		slog.ErrorContext(ctx, "character name resolution failed",
			"request_id", requestID, "count", len(uniqueIDs), "error", err)
		return nil, oops.Code("INTERNAL").Wrap(err)
	}

	// Assemble entries; skip any without a resolved name (graceful degradation).
	entries := make([]*corev1.PresenceEntry, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		name, ok := names[id]
		if !ok || name == "" {
			slog.WarnContext(ctx, "presence entry skipped — name unresolved",
				"request_id", requestID, "character_id", id.String())
			continue
		}
		entries = append(entries, &corev1.PresenceEntry{
			CharacterId:   id.String(),
			CharacterName: name,
			State:         corev1.PresenceState_PRESENCE_STATE_ACTIVE,
		})
	}

	return &corev1.ListFocusPresenceResponse{
		Meta:      responseMeta(requestID),
		Context:   corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION,
		ContextId: info.LocationID.String(),
		Entries:   entries,
	}, nil
}
