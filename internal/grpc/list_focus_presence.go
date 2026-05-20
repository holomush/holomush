// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

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
func (s *CoreServer) ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
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
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "list focus presence ownership validation failed",
			"request_id", requestID, "session_id", req.SessionId, "error", err)
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", req.SessionId).
			Errorf("session not found")
	}

	// TODO(holomush-5b2j): T6 adds expired/empty-location/focus dispatch;
	// T7 adds ABAC gate + store query + name resolution + response.
	return nil, oops.Code("UNIMPLEMENTED").Errorf("not implemented")
}
