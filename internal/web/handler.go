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
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

const (
	// rpcTimeout is the per-call timeout for unary gRPC RPCs.
	rpcTimeout = 10 * time.Second
)

// errStreamClosed is a sentinel to signal that the stream was closed by the server.
var errStreamClosed = errors.New("stream closed")

// CoreClient is the gRPC interface used by Handler to communicate with the
// core service.
type CoreClient interface {
	Authenticate(ctx context.Context, req *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error)
	HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
	Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
	GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error)
	// Auth RPCs (two-phase login)
	AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error)
	SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error)
	CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error)
	CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error)
	ListCharacters(ctx context.Context, req *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error)
	RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error)
	ConfirmPasswordReset(ctx context.Context, req *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error)
	Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error)
}

// ContentClient is the gRPC interface used by Handler to communicate with the
// content service.
type ContentClient interface {
	GetContent(ctx context.Context, req *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error)
	ListContent(ctx context.Context, req *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error)
}

// Handler implements WebServiceHandler by delegating to the core gRPC client.
// The gateway is a protocol translation layer only — it MUST NOT access
// WorldService or other internal services directly. All game state flows
// through core server RPCs.
type Handler struct {
	client        CoreClient
	contentClient ContentClient
	sessionStore  session.Store
	tokenRepo     auth.PlayerTokenRepository
	verbRegistry  *core.VerbRegistry
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

// WithContentClient sets the content service client for content RPCs.
func WithContentClient(c ContentClient) HandlerOption {
	return func(h *Handler) { h.contentClient = c }
}

// WithVerbRegistry sets the verb registry for event type translation.
func WithVerbRegistry(r *core.VerbRegistry) HandlerOption {
	return func(h *Handler) { h.verbRegistry = r }
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
	slog.DebugContext(ctx, "web: Login", "username", req.Msg.GetUsername())

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
	slog.DebugContext(ctx, "web: SendCommand",
		"session_id", req.Msg.GetSessionId(),
		"command", req.Msg.GetText(),
	)

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
	slog.DebugContext(ctx, "web: StreamEvents", "session_id", req.Msg.GetSessionId())

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
				// Call Disconnect on core — this removes the connection AND
				// runs session lifecycle logic (detach/delete based on
				// remaining connection count). Using a fresh context because
				// the stream context is already cancelled.
				disconnCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, disconnErr := h.client.Disconnect(disconnCtx, &corev1.DisconnectRequest{
					SessionId:    sessionID,
					ConnectionId: connID.String(),
				}); disconnErr != nil {
					slog.Warn("web: disconnect RPC failed on stream close",
						"session_id", sessionID,
						"connection_id", connID.String(),
						"error", disconnErr,
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

	// Pump upstream events in a goroutine. The main loop selects on the
	// recv channel, ctx.Done(), and a heartbeat timer. For HTTP/2 (TLS),
	// ReadIdleTimeout + PingTimeout on the server also detects dead
	// connections. For HTTP/1.1 (dev mode), the heartbeat write is the
	// only way to detect a dead peer — Send() fails with broken pipe.
	type recvResult struct {
		resp *corev1.SubscribeResponse
		err  error
	}
	recvCh := make(chan recvResult)
	go func() {
		defer close(recvCh)
		for {
			resp, recvErr := sub.Recv()
			recvCh <- recvResult{resp, recvErr}
			if recvErr != nil {
				return
			}
		}
	}()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			// Probe the connection — if the client is gone, Send fails.
			if sendErr := stream.Send(&webv1.StreamEventsResponse{
				Frame: &webv1.StreamEventsResponse_Control{
					Control: &webv1.ControlFrame{
						Signal: webv1.ControlSignal_CONTROL_SIGNAL_UNSPECIFIED,
					},
				},
			}); sendErr != nil {
				return nil
			}

		case result, ok := <-recvCh:
			if !ok {
				return nil
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) ||
					errors.Is(result.err, context.Canceled) ||
					errors.Is(result.err, context.DeadlineExceeded) {
					return nil
				}
				slog.WarnContext(ctx, "web: event stream recv error", "session_id", sessionID, "error", result.err)
				return connect.NewError(connect.CodeUnavailable,
					oops.With("session_id", sessionID).Wrap(result.err))
			}
			if fwdErr := h.forwardFrame(ctx, result.resp, stream, sessionID); fwdErr != nil {
				if errors.Is(fwdErr, errStreamClosed) {
					return nil
				}
				return fwdErr
			}
		}
	}
}

// forwardFrame translates and sends a single upstream frame to the web client.
func (h *Handler) forwardFrame(
	ctx context.Context,
	resp *corev1.SubscribeResponse,
	stream *connect.ServerStream[webv1.StreamEventsResponse],
	sessionID string,
) error {
	switch frame := resp.GetFrame().(type) {
	case *corev1.SubscribeResponse_Event:
		gameEvent := h.translateEvent(frame.Event)
		if gameEvent == nil {
			return nil
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
			return errStreamClosed
		}
	}
	return nil
}

// Disconnect ends the session on a best-effort basis; errors are logged but
// never returned to the caller.
func (h *Handler) Disconnect(ctx context.Context, req *connect.Request[webv1.DisconnectRequest]) (*connect.Response[webv1.DisconnectResponse], error) {
	slog.DebugContext(ctx, "web: Disconnect", "session_id", req.Msg.GetSessionId())

	discCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.client.Disconnect(discCtx, &corev1.DisconnectRequest{
		SessionId: req.Msg.GetSessionId(),
	}); err != nil {
		slog.Error("web: disconnect RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
	}

	return connect.NewResponse(&webv1.DisconnectResponse{}), nil
}

// GetCommandHistory returns the command history for a session.
// Proxies through the core gRPC service (gateway boundary invariant).
// TODO: Add full authorization when two-phase login is implemented —
// verify the caller's player token owns the requested session.
func (h *Handler) GetCommandHistory(ctx context.Context, req *connect.Request[webv1.GetCommandHistoryRequest]) (*connect.Response[webv1.GetCommandHistoryResponse], error) {
	slog.DebugContext(ctx, "web: GetCommandHistory", "session_id", req.Msg.GetSessionId())

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

// WebGetContent retrieves a single content item by key.
func (h *Handler) WebGetContent(ctx context.Context, req *connect.Request[webv1.WebGetContentRequest]) (*connect.Response[webv1.WebGetContentResponse], error) {
	slog.DebugContext(ctx, "web: WebGetContent", "key", req.Msg.GetKey())

	if h.contentClient == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("content client not configured"))
	}
	callCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.contentClient.GetContent(callCtx, &contentv1.GetContentRequest{
		Key: req.Msg.GetKey(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	item := resp.GetItem()
	if item == nil {
		return connect.NewResponse(&webv1.WebGetContentResponse{}), nil
	}
	return connect.NewResponse(&webv1.WebGetContentResponse{
		Item: &webv1.WebContentItem{
			Key:         item.GetKey(),
			ContentType: item.GetContentType(),
			Body:        item.GetBody(),
			Metadata:    item.GetMetadata(),
		},
	}), nil
}

// WebListContent returns content items matching a key prefix.
func (h *Handler) WebListContent(ctx context.Context, req *connect.Request[webv1.WebListContentRequest]) (*connect.Response[webv1.WebListContentResponse], error) {
	slog.DebugContext(ctx, "web: WebListContent", "prefix", req.Msg.GetPrefix())

	if h.contentClient == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("content client not configured"))
	}
	callCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.contentClient.ListContent(callCtx, &contentv1.ListContentRequest{
		Prefix: req.Msg.GetPrefix(),
		Limit:  req.Msg.GetLimit(),
		Cursor: req.Msg.GetCursor(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	items := make([]*webv1.WebContentItem, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		items = append(items, &webv1.WebContentItem{
			Key:         item.GetKey(),
			ContentType: item.GetContentType(),
			Body:        item.GetBody(),
			Metadata:    item.GetMetadata(),
		})
	}
	return connect.NewResponse(&webv1.WebListContentResponse{
		Items:      items,
		NextCursor: resp.GetNextCursor(),
	}), nil
}
