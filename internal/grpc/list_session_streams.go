// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ListSessionStreams returns the stream names the session is subscribed to,
// derived from focusCoordinator.RestoreFocus. Pure read — does not mutate
// session state. Auth follows the QueryStreamHistory pattern: session
// existence + expiry only; no player_session_token required.
func (s *CoreServer) ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}
	slog.DebugContext(ctx, "list session streams",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}

	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_NOT_FOUND").
				With("session_id", req.SessionId).
				Errorf("session not found")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").
			With("session_id", req.SessionId).
			Errorf("session expired")
	}

	var plan focus.RestorePlan
	if s.focusCoordinator != nil {
		var planErr error
		plan, planErr = s.focusCoordinator.RestoreFocus(ctx, req.SessionId)
		if planErr != nil {
			slog.WarnContext(ctx, "RestoreFocus failed, falling back to empty plan",
				"session_id", req.SessionId, "error", planErr)
		}
	}

	// Fallback: replicate Subscribe's focusCoordinator-nil ambient-stream
	// assembly (server.go:787-816) so this RPC never returns a different
	// stream set than Subscribe under any server configuration.
	if len(plan.Streams) == 0 {
		plan.Streams = append(plan.Streams,
			focus.StreamWithMode{Stream: world.CharacterStream(info.CharacterID), Mode: focus.ReplayModeFromCursor},
		)
		if !info.LocationID.IsZero() {
			plan.Streams = append(plan.Streams,
				focus.StreamWithMode{Stream: world.LocationStream(info.LocationID), Mode: focus.ReplayModeFromCursor},
			)
		}
		if s.streamContributor != nil {
			playerID := ""
			if !info.PlayerID.IsZero() {
				playerID = info.PlayerID.String()
			}
			pluginStreams := s.streamContributor.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
				CharacterID: info.CharacterID.String(),
				PlayerID:    playerID,
				SessionID:   info.ID,
			})
			for _, ps := range pluginStreams {
				plan.Streams = append(plan.Streams,
					focus.StreamWithMode{Stream: ps, Mode: focus.ReplayModeFromCursor},
				)
			}
		}
	}

	out := make([]string, 0, len(plan.Streams))
	for _, sm := range plan.Streams {
		out = append(out, sm.Stream)
	}
	return &corev1.ListSessionStreamsResponse{
		Meta:    responseMeta(requestID),
		Streams: out,
	}, nil
}
