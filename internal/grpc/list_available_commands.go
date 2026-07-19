// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ListAvailableCommands returns the commands the session's own character may
// execute. Self-scoped (the subject is the session character, never an arbitrary
// id) per design INV-COMMAND-3. Ownership failures collapse to SESSION_NOT_FOUND, mirroring
// ListFocusPresence.
func (h *QueryHandler) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	if req == nil {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("request is required")
	}
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}
	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}
	if _, err := auth.ValidateSessionOwnership(ctx, h.playerSessionRepo, h.sessionStore, req.GetPlayerSessionToken(), req.GetSessionId()); err != nil {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}
	info, err := h.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").With("session_id", req.SessionId).Errorf("session expired")
	}
	if h.commandQuerier == nil {
		slog.ErrorContext(ctx, "list available commands: querier not configured", "request_id", requestID, "session_id", req.SessionId)
		return nil, oops.Code("PERMISSION_DENIED").With("reason", "command_querier_not_configured").Errorf("permission denied")
	}
	res, err := h.commandQuerier.Available(ctx, access.CharacterSubject(info.CharacterID.String()))
	if err != nil {
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	out := make([]*corev1.AvailableCommand, 0, len(res.Commands))
	for i := range res.Commands {
		out = append(out, &corev1.AvailableCommand{
			Name: res.Commands[i].Name, Help: res.Commands[i].Help,
			Usage: res.Commands[i].Usage, Source: res.Commands[i].Source,
		})
	}
	return &corev1.ListAvailableCommandsResponse{
		Meta: responseMeta(requestID), Commands: out, Aliases: res.Aliases, Incomplete: res.Incomplete,
	}, nil
}
