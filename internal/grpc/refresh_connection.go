// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// RefreshConnection bumps a connection's liveness lease (holomush-rsoe6).
// Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe, I-SEC-1).
func (s *CoreServer) RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	if req.GetSessionId() == "" || req.GetConnectionId() == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id and connection_id are required")
	}
	if _, err := auth.ValidateSessionOwnership(
		ctx, s.playerSessionRepo, s.sessionStore,
		req.GetPlayerSessionToken(), req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "refresh connection ownership validation failed",
			"session_id", req.GetSessionId(), "error", err)
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", req.GetSessionId()).Errorf("session not found")
	}
	connID, err := ulid.Parse(req.GetConnectionId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("connection_id", req.GetConnectionId()).
			Errorf("connection_id is not a valid ULID")
	}
	if refreshErr := s.sessionStore.RefreshConnection(ctx, connID); refreshErr != nil {
		return nil, refreshErr //nolint:wrapcheck // store returns canonical CONNECTION_NOT_FOUND oops code
	}
	return &corev1.RefreshConnectionResponse{Meta: responseMeta(req.GetMeta().GetRequestId())}, nil
}
