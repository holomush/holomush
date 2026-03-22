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
	"github.com/holomush/holomush/internal/core"
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
	Authenticate(ctx context.Context, req *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error)
	HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
	Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
	GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error)
}

// Handler implements WebServiceHandler by delegating to the core gRPC client.
// The gateway is a protocol translation layer only — it MUST NOT access
// WorldService or other internal services directly. All game state flows
// through core server RPCs.
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

	resp, err := h.client.Authenticate(authCtx, &corev1.AuthenticateRequest{
		Username:   req.Msg.GetUsername(),
		Password:   req.Msg.GetPassword(),
		ClientType: "terminal",
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
// Command history persistence is handled server-side by HandleCommand
// (see grpc/server.go AppendCommand call), so no additional work is
// needed here.
func (h *Handler) SendCommand(ctx context.Context, req *connect.Request[webv1.SendCommandRequest]) (*connect.Response[webv1.SendCommandResponse], error) {
	cmdCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
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
		ErrorMessage: resp.GetError(),
	}), nil
}

// StreamEvents subscribes to core events for a session and forwards them to
// the client as GameEvent messages. Registers a connection for the duration
// of the stream and cleans it up when the stream closes.
func (h *Handler) StreamEvents(ctx context.Context, req *connect.Request[webv1.StreamEventsRequest], stream *connect.ServerStream[webv1.StreamEventsResponse]) error {
	sessionID := req.Msg.GetSessionId()

	// Register connection for the duration of the stream
	if h.sessionStore != nil {
		connID := core.NewULID()
		// ClientType is "terminal" because StreamEvents is the terminal-mode
		// streaming endpoint. A future comms_hub client type would be registered
		// via a separate route.
		conn := &session.Connection{
			ID:          connID,
			SessionID:   sessionID,
			ClientType:  "terminal",
			ConnectedAt: time.Now(),
		}
		if err := h.sessionStore.AddConnection(ctx, conn); err != nil {
			slog.WarnContext(ctx, "web: failed to register stream connection",
				"session_id", sessionID,
				"error", err,
			)
		} else {
			defer func() {
				if removeErr := h.sessionStore.RemoveConnection(context.Background(), connID); removeErr != nil {
					slog.Warn("web: failed to remove stream connection",
						"session_id", sessionID,
						"connection_id", connID.String(),
						"error", removeErr,
					)
				}
			}()
		}
	}

	// Synthetic location_state is injected by the core server's Subscribe handler
	// (which has direct access to WorldService). The gateway just forwards it.

	sub, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
		SessionId:        sessionID,
		ReplayFromCursor: req.Msg.GetReplayFromCursor(),
	})
	if err != nil {
		return connect.NewError(connect.CodeInternal,
			oops.With("session_id", sessionID).Wrap(err))
	}

	for {
		resp, recvErr := sub.Recv()
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

		switch frame := resp.GetFrame().(type) {
		case *corev1.SubscribeResponse_Event:
			gameEvent := translateEvent(frame.Event)
			if gameEvent == nil {
				continue
			}
			if sendErr := stream.Send(&webv1.StreamEventsResponse{
				Frame: &webv1.StreamEventsResponse_Event{Event: gameEvent},
			}); sendErr != nil {
				if errors.Is(sendErr, context.Canceled) ||
					errors.Is(sendErr, context.DeadlineExceeded) {
					return nil
				}
				slog.WarnContext(ctx, "web: stream send error", "session_id", sessionID, "error", sendErr)
				return connect.NewError(connect.CodeUnavailable,
					oops.With("session_id", sessionID).Wrap(sendErr))
			}
		case *corev1.SubscribeResponse_Control:
			if sendErr := stream.Send(&webv1.StreamEventsResponse{
				Frame: &webv1.StreamEventsResponse_Control{
					Control: &webv1.ControlFrame{
						Signal:  webv1.ControlSignal(frame.Control.GetSignal()),
						Message: frame.Control.GetMessage(),
					},
				},
			}); sendErr != nil {
				if errors.Is(sendErr, context.Canceled) ||
					errors.Is(sendErr, context.DeadlineExceeded) {
					return nil
				}
				slog.WarnContext(ctx, "web: stream send error", "session_id", sessionID, "error", sendErr)
				return connect.NewError(connect.CodeUnavailable,
					oops.With("session_id", sessionID).Wrap(sendErr))
			}
			if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
				return nil
			}
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

// WebAuthenticatePlayer validates player credentials and returns a player token with character list.
func (h *Handler) WebAuthenticatePlayer(_ context.Context, _ *connect.Request[webv1.WebAuthenticatePlayerRequest]) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebSelectCharacter selects a character and creates or reattaches a game session.
func (h *Handler) WebSelectCharacter(_ context.Context, _ *connect.Request[webv1.WebSelectCharacterRequest]) (*connect.Response[webv1.WebSelectCharacterResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebCreatePlayer creates a new player account.
func (h *Handler) WebCreatePlayer(_ context.Context, _ *connect.Request[webv1.WebCreatePlayerRequest]) (*connect.Response[webv1.WebCreatePlayerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebCreateCharacter creates a new character for the authenticated player.
func (h *Handler) WebCreateCharacter(_ context.Context, _ *connect.Request[webv1.WebCreateCharacterRequest]) (*connect.Response[webv1.WebCreateCharacterResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebListCharacters returns the characters available for the authenticated player.
func (h *Handler) WebListCharacters(_ context.Context, _ *connect.Request[webv1.WebListCharactersRequest]) (*connect.Response[webv1.WebListCharactersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebLogout ends the current session.
func (h *Handler) WebLogout(_ context.Context, _ *connect.Request[webv1.WebLogoutRequest]) (*connect.Response[webv1.WebLogoutResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebRequestPasswordReset initiates a password reset flow.
func (h *Handler) WebRequestPasswordReset(_ context.Context, _ *connect.Request[webv1.WebRequestPasswordResetRequest]) (*connect.Response[webv1.WebRequestPasswordResetResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// WebConfirmPasswordReset completes a password reset using a token.
func (h *Handler) WebConfirmPasswordReset(_ context.Context, _ *connect.Request[webv1.WebConfirmPasswordResetRequest]) (*connect.Response[webv1.WebConfirmPasswordResetResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not yet implemented"))
}

// GetCommandHistory returns the command history for a session.
// Proxies through the core gRPC service (gateway boundary invariant).
// TODO: Add full authorization when two-phase login is implemented —
// verify the caller's player token owns the requested session.
func (h *Handler) GetCommandHistory(ctx context.Context, req *connect.Request[webv1.GetCommandHistoryRequest]) (*connect.Response[webv1.GetCommandHistoryResponse], error) {
	cmdCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.GetCommandHistory(cmdCtx, &corev1.GetCommandHistoryRequest{
		SessionId: req.Msg.GetSessionId(),
	})
	if err != nil {
		slog.Error("web: get command history RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
		return connect.NewResponse(&webv1.GetCommandHistoryResponse{}), nil
	}

	if !resp.GetSuccess() {
		return connect.NewResponse(&webv1.GetCommandHistoryResponse{}), nil
	}

	return connect.NewResponse(&webv1.GetCommandHistoryResponse{
		Commands: resp.GetCommands(),
	}), nil
}
