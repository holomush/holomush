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

	"github.com/holomush/holomush/internal/core"
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
	CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error)
	CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error)
	QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error)
	ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error)
	// Session management RPCs
	ListPlayerSessions(ctx context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error)
	RevokePlayerSession(ctx context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error)
	RevokeOtherPlayerSessions(ctx context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error)
	// Presence RPCs
	ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error)
	// Command introspection
	ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error)
	// Liveness RPCs
	RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error)
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
// through core server RPCs. The gateway process can run without any
// database credentials (bd-j2xj); per-connection registration happens
// inside the core Subscribe RPC.
type Handler struct {
	client        CoreClient
	contentClient ContentClient
	// heartbeatInterval controls the StreamEvents heartbeat ticker period.
	// Zero means 15 seconds (production default). Overridable in tests.
	heartbeatInterval time.Duration
}

// compile-time check that Handler satisfies the generated interface.
var _ webv1connect.WebServiceHandler = (*Handler)(nil)

// HandlerOption configures optional Handler dependencies.
type HandlerOption func(*Handler)

// WithContentClient sets the content service client for content RPCs.
func WithContentClient(c ContentClient) HandlerOption {
	return func(h *Handler) { h.contentClient = c }
}

// NewHandler creates a new Handler with the given core client and options.
func NewHandler(client CoreClient, opts ...HandlerOption) *Handler {
	h := &Handler{client: client}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// SendCommand forwards a game command to the core service.
// Command history persistence is handled server-side by HandleCommand
// (see grpc/server.go AppendCommand call), so no additional work is
// needed here.
func (h *Handler) SendCommand(ctx context.Context, req *connect.Request[webv1.SendCommandRequest]) (*connect.Response[webv1.SendCommandResponse], error) {
	slog.DebugContext(
		ctx, "web: SendCommand",
		"session_id", req.Msg.GetSessionId(),
		"command", req.Msg.GetText(),
	)

	// Read the token directly (Get returns "" if absent). Server-side
	// validation (Tasks 9-12) will reject empty tokens; until then an
	// empty value is harmless and we don't want to bounce the caller
	// from the gateway.
	token := req.Header().Get(headerInjectSessionToken)

	cmdCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	// Client supplies its own connection_id per request (Phase 5; see
	// the STREAM_OPENED ControlFrame the server emits on StreamEvents
	// open). Empty when the caller isn't routing per-connection (e.g.,
	// scripted/admin paths). Server gracefully handles zero ULID.
	connIDStr := req.Msg.GetConnectionId()

	resp, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
		SessionId:          req.Msg.GetSessionId(),
		Command:            req.Msg.GetText(),
		PlayerSessionToken: token,
		ConnectionId:       connIDStr,
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: handle command RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
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
// the client as GameEvent messages. A per-stream connection_id is generated
// locally and passed to core's Subscribe RPC, which performs the actual
// session-store registration/deregistration (bd-j2xj). The gateway no
// longer touches the session store directly.
func (h *Handler) StreamEvents(ctx context.Context, req *connect.Request[webv1.StreamEventsRequest], stream *connect.ServerStream[webv1.StreamEventsResponse]) error {
	slog.DebugContext(ctx, "web: StreamEvents", "session_id", req.Msg.GetSessionId())

	sessionID := req.Msg.GetSessionId()
	// Read the token directly (Get returns "" if absent). Server-side
	// validation (Tasks 9-12) will reject empty tokens; until then an
	// empty value is harmless and we don't want to bounce the caller
	// from the gateway.
	token := req.Header().Get(headerInjectSessionToken)

	// Generate a per-stream connection_id. Core's Subscribe handler
	// registers this connection in the session store and deregisters
	// it on any stream exit (client disconnect, context cancel, error,
	// STREAM_CLOSED). ClientType is "terminal" because StreamEvents is
	// the terminal-mode streaming endpoint.
	connID := core.NewULID()

	// Disconnect on stream exit triggers core-side session lifecycle
	// (detach/delete based on remaining connection count). Connection
	// removal is handled by core's Subscribe defer path; the idempotent
	// RemoveConnection in Disconnect is a no-op by then.
	defer func() {
		disconnCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, disconnErr := h.client.Disconnect(disconnCtx, &corev1.DisconnectRequest{
			SessionId:          sessionID,
			ConnectionId:       connID.String(),
			PlayerSessionToken: token,
		}); disconnErr != nil {
			slog.WarnContext(
				ctx,
				"web: disconnect RPC failed on stream close",
				"session_id", sessionID,
				"connection_id", connID.String(),
				"error", disconnErr,
			)
		}
	}()

	// Synthetic location_state is injected by the core server's Subscribe handler
	// (which has direct access to WorldService). The gateway just forwards it.

	sub, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
		SessionId:          sessionID,
		PlayerSessionToken: token,
		ConnectionId:       connID.String(),
		ClientType:         "terminal",
	})
	if err != nil {
		return connect.NewError(connect.CodeInternal,
			oops.With("session_id", sessionID).Wrap(err))
	}

	// Emit a STREAM_OPENED ControlFrame carrying the per-stream
	// connection_id so the client can include it in subsequent
	// SendCommand requests. This is the routing identity for Phase 5
	// per-connection commands (scene focus / grid autofocus).
	if sendErr := stream.Send(&webv1.StreamEventsResponse{
		Frame: &webv1.StreamEventsResponse_Control{
			Control: &webv1.ControlFrame{
				Signal:       webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED,
				ConnectionId: connID.String(),
			},
		},
	}); sendErr != nil {
		return connect.NewError(connect.CodeInternal,
			oops.With("session_id", sessionID).Wrap(sendErr))
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
	recvCh := make(chan recvResult, 1)
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
	hbInterval := h.heartbeatInterval
	if hbInterval <= 0 {
		hbInterval = 15 * time.Second
	}
	heartbeat := time.NewTicker(hbInterval)
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
			// Refresh the server-side liveness lease for this connection (I-LIVE-1).
			// Failure is transient — log at Debug and keep the stream open.
			refreshCtx, refreshCancel := context.WithTimeout(ctx, rpcTimeout)
			if _, refreshErr := h.client.RefreshConnection(refreshCtx, &corev1.RefreshConnectionRequest{
				SessionId:          sessionID,
				ConnectionId:       connID.String(),
				PlayerSessionToken: token,
			}); refreshErr != nil {
				slog.DebugContext(ctx, "web: lease refresh failed (transient)", "session_id", sessionID, "error", refreshErr)
			}
			refreshCancel()

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
					Signal:         webv1.ControlSignal(frame.Control.GetSignal()),
					Message:        frame.Control.GetMessage(),
					AttachMomentMs: frame.Control.GetAttachMomentMs(),
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

	// Read the token directly (Get returns "" if absent). Server-side
	// validation (Tasks 9-12) will reject empty tokens; until then an
	// empty value is harmless and we don't want to bounce the caller
	// from the gateway.
	token := req.Header().Get(headerInjectSessionToken)

	discCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.client.Disconnect(discCtx, &corev1.DisconnectRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
	}); err != nil {
		slog.ErrorContext(ctx, "web: disconnect RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
	}

	return connect.NewResponse(&webv1.DisconnectResponse{}), nil
}

// GetCommandHistory returns the command history for a session.
// Proxies through the core gRPC service (gateway boundary invariant).
// Authorization (bd-jv7z) is enforced server-side in CoreServer via
// auth.ValidateSessionOwnership; the gateway just forwards the
// player_session_token header.
func (h *Handler) GetCommandHistory(ctx context.Context, req *connect.Request[webv1.GetCommandHistoryRequest]) (*connect.Response[webv1.GetCommandHistoryResponse], error) {
	slog.DebugContext(ctx, "web: GetCommandHistory", "session_id", req.Msg.GetSessionId())

	// Forward the session token header. The core server enforces
	// ownership; an empty or wrong token produces a success=false
	// response which we surface as an empty history payload below.
	token := req.Header().Get(headerInjectSessionToken)

	cmdCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.GetCommandHistory(cmdCtx, &corev1.GetCommandHistoryRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: get command history RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
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

// WebQueryStreamHistory proxies paginated event history requests to CoreService.
// Authorization is enforced by the core service; the gateway is a translation layer.
func (h *Handler) WebQueryStreamHistory(ctx context.Context, req *connect.Request[webv1.WebQueryStreamHistoryRequest]) (*connect.Response[webv1.WebQueryStreamHistoryResponse], error) {
	slog.DebugContext(
		ctx, "web: WebQueryStreamHistory",
		"session_id", req.Msg.GetSessionId(),
		"stream", req.Msg.GetStream(),
	)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.QueryStreamHistory(rpcCtx, &corev1.QueryStreamHistoryRequest{
		SessionId:   req.Msg.GetSessionId(),
		Stream:      req.Msg.GetStream(),
		Count:       req.Msg.GetCount(),
		NotBeforeMs: req.Msg.GetNotBeforeMs(),
		Cursor:      req.Msg.GetCursor(),
		NotAfterMs:  req.Msg.GetNotAfterMs(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: query stream history RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is so clients can distinguish STREAM_ACCESS_DENIED / INVALID_ARGUMENT. (Per I-PRIV-5 wire-opacity, expired-session + missing-session denials now collapse into STREAM_ACCESS_DENIED upstream.)
	}

	gameEvents := make([]*webv1.GameEvent, 0, len(resp.GetEvents()))
	for _, ef := range resp.GetEvents() {
		ge := h.translateEvent(ef)
		if ge != nil {
			ge.Cursor = ef.GetCursor()
			gameEvents = append(gameEvents, ge)
		}
	}

	return connect.NewResponse(&webv1.WebQueryStreamHistoryResponse{
		Events:     gameEvents,
		HasMore:    resp.GetHasMore(),
		NextCursor: resp.GetNextCursor(),
	}), nil
}

// WebListSessionStreams proxies stream enumeration requests to CoreService.
// Authorization (bd-jv7z) is enforced server-side in CoreServer via
// auth.ValidateSessionOwnership; the gateway just forwards the
// player_session_token header.
func (h *Handler) WebListSessionStreams(ctx context.Context, req *connect.Request[webv1.WebListSessionStreamsRequest]) (*connect.Response[webv1.WebListSessionStreamsResponse], error) {
	slog.DebugContext(
		ctx, "web: WebListSessionStreams",
		"session_id", req.Msg.GetSessionId(),
	)

	// Forward the session token header. The core server enforces
	// ownership; an empty or wrong token collapses to SESSION_NOT_FOUND.
	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.ListSessionStreams(rpcCtx, &corev1.ListSessionStreamsRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: list session streams RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish SESSION_EXPIRED / SESSION_NOT_FOUND / INVALID_ARGUMENT. ListSessionStreams is NOT governed by I-PRIV-5 wire-opacity (that invariant applies only to QueryStreamHistory denial paths); distinct codes here are intentional.
	}

	return connect.NewResponse(&webv1.WebListSessionStreamsResponse{
		Streams: resp.GetStreams(),
	}), nil
}

// WebListFocusPresence forwards to CoreService.ListFocusPresence. The
// player_session_token is read from the request header (not the wire
// request body); core enforces ownership.
func (h *Handler) WebListFocusPresence(ctx context.Context, req *connect.Request[webv1.WebListFocusPresenceRequest]) (*connect.Response[webv1.WebListFocusPresenceResponse], error) {
	slog.DebugContext(ctx, "web: WebListFocusPresence",
		"session_id", req.Msg.GetSessionId())

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListFocusPresence(rpcCtx, &corev1.ListFocusPresenceRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: list focus presence RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish PERMISSION_DENIED / UNIMPLEMENTED / SESSION_NOT_FOUND.
	}

	return connect.NewResponse(&webv1.WebListFocusPresenceResponse{
		Context:   translatePresenceContext(coreResp.GetContext()),
		ContextId: coreResp.GetContextId(),
		Entries:   translatePresenceEntries(coreResp.GetEntries()),
	}), nil
}

// WebListCommands proxies to CoreService.ListAvailableCommands. The
// player_session_token is read from the request header (not the wire
// request body); core enforces ownership and ABAC filtering.
func (h *Handler) WebListCommands(ctx context.Context, req *connect.Request[webv1.WebListCommandsRequest]) (*connect.Response[webv1.WebListCommandsResponse], error) {
	slog.DebugContext(ctx, "web: WebListCommands", "session_id", req.Msg.GetSessionId())

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListAvailableCommands(rpcCtx, &corev1.ListAvailableCommandsRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: list available commands RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish SESSION_NOT_FOUND / PERMISSION_DENIED.
	}

	out := make([]*webv1.WebAvailableCommand, 0, len(coreResp.GetCommands()))
	for _, c := range coreResp.GetCommands() {
		if c == nil {
			continue
		}
		out = append(out, &webv1.WebAvailableCommand{
			Name:   c.GetName(),
			Help:   c.GetHelp(),
			Usage:  c.GetUsage(),
			Source: c.GetSource(),
		})
	}

	return connect.NewResponse(&webv1.WebListCommandsResponse{
		Commands:   out,
		Aliases:    coreResp.GetAliases(),
		Incomplete: coreResp.GetIncomplete(),
	}), nil
}

func translatePresenceContext(c corev1.PresenceContext) webv1.WebPresenceContext {
	switch c {
	case corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION:
		return webv1.WebPresenceContext_WEB_PRESENCE_CONTEXT_LOCATION
	case corev1.PresenceContext_PRESENCE_CONTEXT_SCENE:
		return webv1.WebPresenceContext_WEB_PRESENCE_CONTEXT_SCENE
	default:
		return webv1.WebPresenceContext_WEB_PRESENCE_CONTEXT_UNSPECIFIED
	}
}

func translatePresenceState(s corev1.PresenceState) webv1.WebPresenceState {
	switch s {
	case corev1.PresenceState_PRESENCE_STATE_ACTIVE:
		return webv1.WebPresenceState_WEB_PRESENCE_STATE_ACTIVE
	case corev1.PresenceState_PRESENCE_STATE_DETACHED:
		return webv1.WebPresenceState_WEB_PRESENCE_STATE_DETACHED
	case corev1.PresenceState_PRESENCE_STATE_INACTIVE:
		return webv1.WebPresenceState_WEB_PRESENCE_STATE_INACTIVE
	default:
		return webv1.WebPresenceState_WEB_PRESENCE_STATE_UNSPECIFIED
	}
}

func translatePresenceEntries(in []*corev1.PresenceEntry) []*webv1.WebPresenceEntry {
	out := make([]*webv1.WebPresenceEntry, 0, len(in))
	for _, e := range in {
		// Defense: skip nil entries. Protobuf Get* methods are nil-safe (would
		// return zero values), so this only prevents emitting an entry with
		// empty character_id / character_name to clients. Core never sends
		// nil entries today; this is belt-and-suspenders against future drift.
		if e == nil {
			continue
		}
		out = append(out, &webv1.WebPresenceEntry{
			CharacterId:   e.GetCharacterId(),
			CharacterName: e.GetCharacterName(),
			State:         translatePresenceState(e.GetState()),
		})
	}
	return out
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
