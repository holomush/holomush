// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package web implements the ConnectRPC WebService handler, bridging HTTP/2
// clients to the HoloMUSH core via gRPC.
package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

const (
	// rpcTimeout is the per-call timeout for unary gRPC RPCs.
	rpcTimeout = 10 * time.Second
)

// CoreClient is the gRPC interface used by Handler to communicate with the
// core service.
type CoreClient interface {
	Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error)
	HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error)
	Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error)
	Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
}

// Handler implements WebServiceHandler by delegating to the core gRPC client.
type Handler struct {
	client CoreClient
}

// compile-time check that Handler satisfies the generated interface.
var _ webv1connect.WebServiceHandler = (*Handler)(nil)

// NewHandler creates a new Handler with the given core client.
func NewHandler(client CoreClient) *Handler {
	return &Handler{client: client}
}

// Login authenticates a user and returns session details.
func (h *Handler) Login(ctx context.Context, req *connect.Request[webv1.LoginRequest]) (*connect.Response[webv1.LoginResponse], error) {
	authCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.Authenticate(authCtx, &corev1.AuthRequest{
		Username: req.Msg.GetUsername(),
		Password: req.Msg.GetPassword(),
	})
	if err != nil {
		slog.Error("web: authenticate RPC failed", "error", err)
		return connect.NewResponse(&webv1.LoginResponse{
			Success:      false,
			ErrorMessage: "authentication error",
		}), nil
	}

	if !resp.GetSuccess() {
		return connect.NewResponse(&webv1.LoginResponse{
			Success:      false,
			ErrorMessage: resp.GetError(),
		}), nil
	}

	return connect.NewResponse(&webv1.LoginResponse{
		Success:       true,
		SessionId:     resp.GetSessionId(),
		CharacterName: resp.GetCharacterName(),
	}), nil
}

// SendCommand forwards a game command to the core service.
func (h *Handler) SendCommand(ctx context.Context, req *connect.Request[webv1.SendCommandRequest]) (*connect.Response[webv1.SendCommandResponse], error) {
	cmdCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.HandleCommand(cmdCtx, &corev1.CommandRequest{
		SessionId: req.Msg.GetSessionId(),
		Command:   req.Msg.GetText(),
	})
	if err != nil {
		slog.Error("web: handle command RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
		return connect.NewResponse(&webv1.SendCommandResponse{
			Success:      false,
			ErrorMessage: "command error",
		}), nil
	}

	return connect.NewResponse(&webv1.SendCommandResponse{
		Success:      resp.GetSuccess(),
		Output:       resp.GetOutput(),
		ErrorMessage: resp.GetError(),
	}), nil
}

// StreamEvents subscribes to core events for a session and forwards them to
// the client as GameEvent messages.
func (h *Handler) StreamEvents(ctx context.Context, req *connect.Request[webv1.StreamEventsRequest], stream *connect.ServerStream[webv1.StreamEventsResponse]) error {
	sessionID := req.Msg.GetSessionId()

	sub, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
		SessionId: sessionID,
	})
	if err != nil {
		return connect.NewError(connect.CodeInternal,
			oops.With("session_id", sessionID).Wrap(err))
	}

	for {
		ev, recvErr := sub.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) ||
				errors.Is(recvErr, context.Canceled) ||
				errors.Is(recvErr, context.DeadlineExceeded) {
				return nil
			}
			slog.WarnContext(ctx, "web: event stream recv error", "session_id", sessionID, "error", recvErr)
			return connect.NewError(connect.CodeUnavailable,
				oops.With("session_id", sessionID).Wrap(recvErr))
		}

		gameEvent := translateEvent(ev)
		if gameEvent == nil {
			continue
		}

		if sendErr := stream.Send(&webv1.StreamEventsResponse{Event: gameEvent}); sendErr != nil {
			if errors.Is(sendErr, context.Canceled) ||
				errors.Is(sendErr, context.DeadlineExceeded) {
				return nil
			}
			slog.WarnContext(ctx, "web: stream send error", "session_id", sessionID, "error", sendErr)
			return connect.NewError(connect.CodeUnavailable,
				oops.With("session_id", sessionID).Wrap(sendErr))
		}
	}
}

// Disconnect ends the session on a best-effort basis; errors are logged but
// never returned to the caller.
func (h *Handler) Disconnect(ctx context.Context, req *connect.Request[webv1.DisconnectRequest]) (*connect.Response[webv1.DisconnectResponse], error) {
	discCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.client.Disconnect(discCtx, &corev1.DisconnectRequest{
		SessionId: req.Msg.GetSessionId(),
	}); err != nil {
		slog.Error("web: disconnect RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
	}

	return connect.NewResponse(&webv1.DisconnectResponse{}), nil
}
