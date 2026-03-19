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

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/session"
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
	client       CoreClient
	sessionStore session.Store
	tokenRepo    auth.PlayerTokenRepository
}

// compile-time check that Handler satisfies the generated interface.
var _ webv1connect.WebServiceHandler = (*Handler)(nil)

// HandlerOption configures optional Handler dependencies.
type HandlerOption func(*Handler)

// WithSessionStore sets the session store for session-related RPCs.
func WithSessionStore(store session.Store) HandlerOption {
	return func(h *Handler) { h.sessionStore = store }
}

// WithPlayerTokenRepo sets the player token repository for two-phase login RPCs.
func WithPlayerTokenRepo(repo auth.PlayerTokenRepository) HandlerOption {
	return func(h *Handler) { h.tokenRepo = repo }
}

// NewHandler creates a new Handler with the given core client and options.
func NewHandler(client CoreClient, opts ...HandlerOption) *Handler {
	h := &Handler{client: client}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
		SessionId:        sessionID,
		ReplayFromCursor: req.Msg.GetReplayFromCursor(),
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

// AuthenticatePlayer validates player credentials and returns a player token.
// Stub: returns CodeUnimplemented pending full player account system.
func (h *Handler) AuthenticatePlayer(_ context.Context, _ *connect.Request[webv1.AuthenticatePlayerRequest]) (*connect.Response[webv1.AuthenticatePlayerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("two-phase login not yet implemented"))
}

// ListCharacters returns the characters available for an authenticated player.
// Stub: returns CodeUnimplemented pending full player account system.
func (h *Handler) ListCharacters(_ context.Context, _ *connect.Request[webv1.ListCharactersRequest]) (*connect.Response[webv1.ListCharactersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("two-phase login not yet implemented"))
}

// SelectCharacter selects a character and creates or reattaches a game session.
// Stub: returns CodeUnimplemented pending full player account system.
func (h *Handler) SelectCharacter(_ context.Context, _ *connect.Request[webv1.SelectCharacterRequest]) (*connect.Response[webv1.SelectCharacterResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("two-phase login not yet implemented"))
}

// ListSessions returns all sessions for the authenticated player.
// Stub: returns CodeUnimplemented pending full player account system.
func (h *Handler) ListSessions(_ context.Context, _ *connect.Request[webv1.ListSessionsRequest]) (*connect.Response[webv1.ListSessionsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("two-phase login not yet implemented"))
}

// GetCommandHistory returns the command history for a session.
func (h *Handler) GetCommandHistory(ctx context.Context, req *connect.Request[webv1.GetCommandHistoryRequest]) (*connect.Response[webv1.GetCommandHistoryResponse], error) {
	if h.sessionStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("session store not configured"))
	}
	history, err := h.sessionStore.GetCommandHistory(ctx, req.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&webv1.GetCommandHistoryResponse{Commands: history}), nil
}
