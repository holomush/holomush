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
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/telemetry"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

const (
	// rpcTimeout is the per-call timeout for unary gRPC RPCs.
	rpcTimeout = 10 * time.Second

	// defaultReconnectCeiling is the max wall-clock the gateway holds a client
	// open while re-establishing a broken core stream when the caller has not
	// configured an explicit ceiling. Well under the 30m session reattach TTL.
	defaultReconnectCeiling = 2 * time.Minute
)

// tracer emits gateway web-side spans (e.g. the detached lease-refresh trace).
var tracer = otel.Tracer("holomush.web")

// errStreamClosed is a sentinel to signal that the stream was closed by the server.
var errStreamClosed = errors.New("stream closed")

// breakCause classifies why a single Subscribe attempt ended.
type breakCause int

const (
	// breakDone: ctx cancelled or clean server close (EOF / STREAM_CLOSED /
	// terminal SESSION_NOT_FOUND) — StreamEvents returns nil.
	breakDone breakCause = iota
	// breakClientGone: the client transport was lost (a Send to the client
	// failed) — StreamEvents returns nil and the outer defer Disconnect fires.
	breakClientGone
	// breakCoreGone: the core stream errored with a non-EOF transport error
	// while the client is still alive — the gateway re-Subscribes.
	breakCoreGone
)

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

// SceneAccessClient is the gRPC interface used by Handler to reach the
// core scene-access facade. One method per Web* scene RPC, all using
// sceneaccessv1 types. The gateway is a pure translation layer — the
// facade (SceneAccessService) owns all authorization and identity resolution.
type SceneAccessClient interface {
	ListScenesForViewer(ctx context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error)
	GetSceneForViewer(ctx context.Context, req *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error)
	ListMyScenes(ctx context.Context, req *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error)
	WatchScene(ctx context.Context, req *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error)
	ExportScene(ctx context.Context, req *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error)
	SetSceneFocus(ctx context.Context, req *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error)
	ListPublishedScenes(ctx context.Context, req *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error)
	GetPublicSceneArchive(ctx context.Context, req *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error)
	DownloadPublicSceneArchive(ctx context.Context, req *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error)
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
	sceneAccess   SceneAccessClient
	// heartbeatInterval controls the StreamEvents heartbeat ticker period.
	// Zero means 15 seconds (production default). Overridable in tests.
	heartbeatInterval time.Duration
	// reconnectCeiling is the max wall-clock the gateway holds a client while
	// re-establishing a broken core stream; zero → defaultReconnectCeiling.
	// Capped (by convention) at the session reattach TTL.
	reconnectCeiling time.Duration
}

// compile-time check that Handler satisfies the generated interface.
var _ webv1connect.WebServiceHandler = (*Handler)(nil)

// HandlerOption configures optional Handler dependencies.
type HandlerOption func(*Handler)

// WithContentClient sets the content service client for content RPCs.
func WithContentClient(c ContentClient) HandlerOption {
	return func(h *Handler) { h.contentClient = c }
}

// WithSceneAccessClient sets the scene-access facade client for scene Web* RPCs.
func WithSceneAccessClient(c SceneAccessClient) HandlerOption {
	return func(h *Handler) { h.sceneAccess = c }
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

	// Survive a core-stream break: hold the client open across a broken core
	// Subscribe, re-establish the (durable) subscription, dedup the redelivery
	// overlap, and continue — bounded by a reconnect ceiling. (I-SURV-1/2/4.)
	ceiling := h.reconnectCeiling
	if ceiling <= 0 {
		ceiling = defaultReconnectCeiling
	}
	// outageDeadline is the per-outage reconnect budget. Reset to zero on every
	// successful (re)open so a long-healthy stream that later hits a core blip
	// gets a fresh ceiling — the bound is per-outage, not total stream lifetime
	// (I-SURV-4). Zero value means "not currently in an outage".
	var outageDeadline time.Time
	backoff := 100 * time.Millisecond

	// dedup suppresses the redelivery overlap after a reconnect. Core acks
	// JetStream delivery AFTER Send, so only sent-but-unacked frames replay on
	// resume — a small window. A bounded recent-id set (not an unbounded
	// session-lifetime map) keeps per-connection memory fixed regardless of how
	// many distinct events a long-lived stream forwards (holomush-rsoe6.21).
	dedup := newReconnectDedup()
	firstOpen := true

	for {
		cause, opened := h.runSubscribeOnce(ctx, sessionID, token, connID.String(), stream, firstOpen, dedup)
		if opened {
			// A successful (re)open ends any outage: reset the per-outage budget
			// and backoff so a later break gets a fresh ceiling (I-SURV-4 is a
			// per-outage bound, not a total-stream-lifetime bound).
			outageDeadline = time.Time{}
			backoff = 100 * time.Millisecond
		}
		switch cause {
		case breakDone:
			return nil
		case breakClientGone:
			return nil // outer defer Disconnect fires
		case breakCoreGone:
			if outageDeadline.IsZero() {
				outageDeadline = time.Now().Add(ceiling) // start of a new outage
			}
			if time.Now().After(outageDeadline) {
				slog.WarnContext(ctx, "web: core unreachable past reconnect ceiling", "session_id", sessionID)
				return connect.NewError(connect.CodeUnavailable,
					oops.With("session_id", sessionID).Errorf("core unreachable past reconnect ceiling"))
			}
			// Tell the client we are reconnecting; if that Send fails the client
			// is gone, so stop.
			if sendErr := stream.Send(reconnectingControlFrame()); sendErr != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			firstOpen = false
			// loop: re-Subscribe — the durable consumer resumes server-side.
		}
	}
}

// subscribeRecvErrIsTerminal reports whether a core Subscribe recv error is the
// terminal "session is gone" signal (the session was reaped past its reattach
// TTL) rather than a transient core break to reconnect through. Terminal errors
// arrive as a gRPC codes.Unauthenticated or codes.NotFound status — the same
// shape the core server stamps for SESSION_NOT_FOUND on the wire. A transient
// core break (codes.Unavailable, EOF, context cancellation) is NOT terminal and
// must reconnect. This mirrors the client-side SESSION_NOT_FOUND classification
// (internal/grpc TranslateSubscribeErr) without coupling the web package to the
// gRPC client layer.
func subscribeRecvErrIsTerminal(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.NotFound:
		return true
	default:
		return false
	}
}

// runSubscribeOnce runs a single core Subscribe attempt to completion and
// classifies why it ended. On a healthy open it sends STREAM_OPENED (first
// attempt) or RECONNECTED (subsequent attempts), forwards frames (deduping the
// redelivery overlap via the bounded dedup window), and runs the heartbeat/lease
// refresh. It owns a per-attempt context so the spawned recv goroutine is always
// unblocked and reaped when this function returns.
//
// The returned bool (opened) reports whether this attempt successfully opened
// the subscription AND sent its open frame (STREAM_OPENED or RECONNECTED). The
// caller uses it to reset the per-outage reconnect budget on every successful
// (re)open.
func (h *Handler) runSubscribeOnce(
	ctx context.Context,
	sessionID, token, connID string,
	stream *connect.ServerStream[webv1.StreamEventsResponse],
	firstOpen bool,
	dedup *reconnectDedup,
) (breakCause, bool) {
	opened := false
	// Per-attempt context: cancelling it on return unblocks the recv goroutine's
	// sub.Recv() so no goroutine leaks across attempts.
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	sub, err := h.client.Subscribe(subCtx, &corev1.SubscribeRequest{
		SessionId:          sessionID,
		PlayerSessionToken: token,
		ConnectionId:       connID,
		ClientType:         "terminal",
	})
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SESSION_NOT_FOUND" {
			// SESSION_NOT_FOUND = the session was reaped past its reattach TTL.
			// This is intentionally terminal: re-establishing the session needs a
			// SelectCharacter call, but StreamEvents carries only the session
			// token, not the character_id SelectCharacter requires — so by design
			// the client (which holds both) re-authenticates rather than the
			// gateway reattaching in-band. We signal STREAM_CLOSED to prompt that.
			// This branch does NOT fire on a core RESTART (the survival case this
			// reconnect loop exists for): there the session row + durable consumer
			// survive, so re-Subscribe resumes transparently. It fires only when
			// the session is genuinely gone (reaped), where client re-auth is the
			// correct recovery.
			slog.DebugContext(ctx, "web: session not found on subscribe; signalling client to re-auth", "session_id", sessionID)
			if sendErr := stream.Send(&webv1.StreamEventsResponse{
				Frame: &webv1.StreamEventsResponse_Control{
					Control: &webv1.ControlFrame{Signal: webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED},
				},
			}); sendErr != nil {
				// Client already gone; nothing more to do — terminal either way.
				slog.DebugContext(ctx, "web: failed to send STREAM_CLOSED on session-not-found", "session_id", sessionID, "error", sendErr)
			}
			return breakDone, opened
		}
		slog.DebugContext(ctx, "web: core subscribe failed; will reconnect", "session_id", sessionID, "error", err)
		return breakCoreGone, opened
	}

	// Announce the open: STREAM_OPENED (with connID) on the first attempt,
	// RECONNECTED on a resumed attempt.
	var openFrame *webv1.StreamEventsResponse
	if firstOpen {
		openFrame = &webv1.StreamEventsResponse{
			Frame: &webv1.StreamEventsResponse_Control{
				Control: &webv1.ControlFrame{
					Signal:       webv1.ControlSignal_CONTROL_SIGNAL_STREAM_OPENED,
					ConnectionId: connID,
				},
			},
		}
	} else {
		openFrame = reconnectedControlFrame()
	}
	if sendErr := stream.Send(openFrame); sendErr != nil {
		return breakClientGone, opened
	}
	// The subscription is open and the open frame is delivered: the outage (if
	// any) is over. Every return below reports opened=true.
	opened = true

	// Pump upstream events in a goroutine. The main loop selects on the recv
	// channel, ctx.Done(), and a heartbeat timer.
	type recvResult struct {
		resp *corev1.SubscribeResponse
		err  error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		defer close(recvCh)
		for {
			resp, recvErr := sub.Recv()
			select {
			case recvCh <- recvResult{resp, recvErr}:
			case <-subCtx.Done():
				return
			}
			if recvErr != nil {
				return
			}
		}
	}()

	hbInterval := h.heartbeatInterval
	if hbInterval <= 0 {
		hbInterval = session.DefaultLeaseRefreshInterval
	}
	heartbeat := time.NewTicker(hbInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return breakDone, opened

		case <-heartbeat.C:
			// Probe the connection — if the client is gone, Send fails.
			if sendErr := stream.Send(&webv1.StreamEventsResponse{
				Frame: &webv1.StreamEventsResponse_Control{
					Control: &webv1.ControlFrame{
						Signal: webv1.ControlSignal_CONTROL_SIGNAL_UNSPECIFIED,
					},
				},
			}); sendErr != nil {
				return breakClientGone, opened
			}
			// Refresh the server-side liveness lease for this connection (I-LIVE-1).
			// Detach into its own trace root so the periodic refresh does not orphan
			// the long-lived StreamEvents trace (holomush-m7djf). Failure is
			// transient — log at Debug and keep the stream open.
			refreshCtx, refreshSpan := telemetry.DetachTrace(ctx, tracer, "gateway.lease_refresh")
			refreshCtx, refreshCancel := context.WithTimeout(refreshCtx, rpcTimeout)
			if _, refreshErr := h.client.RefreshConnection(refreshCtx, &corev1.RefreshConnectionRequest{
				SessionId:          sessionID,
				ConnectionId:       connID,
				PlayerSessionToken: token,
			}); refreshErr != nil {
				slog.DebugContext(refreshCtx, "web: lease refresh failed (transient)", "session_id", sessionID, "error", refreshErr)
			}
			refreshCancel()
			refreshSpan.End()

		case result, ok := <-recvCh:
			if !ok {
				// recv goroutine ended without a clean STREAM_CLOSED — core likely gone.
				return breakCoreGone, opened
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) ||
					errors.Is(result.err, context.Canceled) ||
					errors.Is(result.err, context.DeadlineExceeded) {
					return breakDone, opened
				}
				// Server-streaming RPCs defer the subscription's ownership error to
				// the first Recv rather than the synchronous Subscribe return, so a
				// session reaped past its reattach TTL surfaces HERE as a
				// codes.Unauthenticated / codes.NotFound status — not above. That is
				// terminal (the client must re-authenticate, exactly as in the
				// synchronous SESSION_NOT_FOUND branch), NOT a transient core break.
				// Signal STREAM_CLOSED so the client re-auths instead of letting the
				// reconnect loop spin for the full ceiling.
				if subscribeRecvErrIsTerminal(result.err) {
					slog.DebugContext(ctx, "web: session not found on recv; signalling client to re-auth", "session_id", sessionID, "error", result.err)
					if sendErr := stream.Send(&webv1.StreamEventsResponse{
						Frame: &webv1.StreamEventsResponse_Control{
							Control: &webv1.ControlFrame{Signal: webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED},
						},
					}); sendErr != nil {
						slog.DebugContext(ctx, "web: failed to send STREAM_CLOSED on recv session-not-found", "session_id", sessionID, "error", sendErr)
					}
					return breakDone, opened
				}
				slog.DebugContext(ctx, "web: core recv errored; will reconnect", "session_id", sessionID, "error", result.err)
				return breakCoreGone, opened
			}
			// Dedup the redelivery overlap before forwarding. Only Event frames
			// carry ids; Control frames pass straight through.
			if ef, isEvent := result.resp.GetFrame().(*corev1.SubscribeResponse_Event); isEvent {
				if id := ef.Event.GetId(); id != "" && dedup.seenOrRecord(id) {
					continue // already forwarded within the recent window; skip the redelivery
				}
			}
			if fwdErr := h.forwardFrame(ctx, result.resp, stream, sessionID); fwdErr != nil {
				if errors.Is(fwdErr, errStreamClosed) {
					return breakDone, opened // server-initiated clean close
				}
				return breakClientGone, opened // client Send failed
			}
		}
	}
}

// reconnectingControlFrame is sent to the client when the gateway has detected a
// core-stream break and is about to re-establish the subscription.
func reconnectingControlFrame() *webv1.StreamEventsResponse {
	return &webv1.StreamEventsResponse{
		Frame: &webv1.StreamEventsResponse_Control{
			Control: &webv1.ControlFrame{Signal: webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTING},
		},
	}
}

// reconnectedControlFrame is sent to the client once the gateway has
// re-established the core subscription after a break.
func reconnectedControlFrame() *webv1.StreamEventsResponse {
	return &webv1.StreamEventsResponse{
		Frame: &webv1.StreamEventsResponse_Control{
			Control: &webv1.ControlFrame{Signal: webv1.ControlSignal_CONTROL_SIGNAL_RECONNECTED},
		},
	}
}

// mapCoreSignalToWeb translates a core ControlSignal enum value to the
// corresponding web ControlSignal. The two enums share the same low values
// (UNSPECIFIED=0, REPLAY_COMPLETE=1, STREAM_CLOSED=2) but diverge above 2 —
// the web proto reserves 3–5 for gateway-synthesised signals (STREAM_OPENED,
// RECONNECTING, RECONNECTED) that have no core counterpart. An explicit switch
// prevents a silent misroute when new values are added to either side.
func mapCoreSignalToWeb(sig corev1.ControlSignal) webv1.ControlSignal {
	switch sig {
	case corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE:
		return webv1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE
	case corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED:
		return webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED
	case corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY:
		return webv1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY
	default:
		return webv1.ControlSignal_CONTROL_SIGNAL_UNSPECIFIED
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
					Signal:         mapCoreSignalToWeb(frame.Control.GetSignal()),
					Message:        frame.Control.GetMessage(),
					AttachMomentMs: frame.Control.GetAttachMomentMs(),
					SceneId:        frame.Control.GetSceneId(),
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
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is so clients can distinguish STREAM_ACCESS_DENIED / INVALID_ARGUMENT. (Per INV-PRIVACY-5 wire-opacity, expired-session + missing-session denials now collapse into STREAM_ACCESS_DENIED upstream.)
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
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish SESSION_EXPIRED / SESSION_NOT_FOUND / INVALID_ARGUMENT. ListSessionStreams is NOT governed by INV-PRIVACY-5 wire-opacity (that invariant applies only to QueryStreamHistory denial paths); distinct codes here are intentional.
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
