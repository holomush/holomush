// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package telnet implements the telnet gateway, connecting raw TCP/telnet
// clients to the HoloMUSH core via gRPC.
package telnet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/gatewaymetrics"
	"github.com/holomush/holomush/internal/grpcclient"
	"github.com/holomush/holomush/internal/telemetry"
	"github.com/holomush/holomush/internal/telnet/gamenotice"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

var tracer = otel.Tracer("holomush.telnet")

const (
	// maxLineSize is the maximum line length from a telnet client (8 KiB).
	maxLineSize = 8 * 1024
	// maxCommandSize is the maximum length of a forwarded command (4 KiB).
	maxCommandSize = 4096
	// rpcTimeout is the per-call timeout for unary gRPC RPCs.
	rpcTimeout = 10 * time.Second
	// drainTimeout bounds drainUntilClosed's wait for the server's
	// STREAM_CLOSED frame before falling back to a client-side message.
	// Matches rpcTimeout so the full quit round-trip (HandleCommand RPC
	// + Subscribe goroutine scheduling + STREAM_CLOSED delivery) has
	// headroom on slow CI runners without stalling a misbehaving server
	// indefinitely.
	drainTimeout = 10 * time.Second
)

// CoreClient is the gRPC interface used by GatewayHandler to communicate with
// the core service.
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
	CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error)
	// Liveness RPCs
	RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error)
}

// GatewayHandler manages a single telnet connection, using gRPC to communicate
// with the core service.
type GatewayHandler struct {
	conn         net.Conn
	reader       *bufio.Reader
	client       CoreClient
	sessionID    string
	connectionID string
	charName     string
	authed       bool
	quitting     bool
	eventCh      chan *corev1.SubscribeResponse

	// lastEventID is the id of the most recently forwarded event; used to drop
	// the single redelivery-overlap frame after a durable-resume reconnect
	// (holomush-rsoe6, I-SURV-5).
	lastEventID string

	// lastSubscribeErr holds the classified error from the most recent stream's
	// receive goroutine (set before the event channel closes). The Handle
	// reconnect loop reads it after a channel close to distinguish a terminal
	// reaped-session SESSION_NOT_FOUND (→ return to picker) from a transient
	// core-down (→ retry). grpc-go defers the Subscribe handler error to the
	// first Recv, so this is the only place the wire code is observable
	// (rsoe6.11.1). Accessed only from the single-threaded Handle goroutine and
	// its trySubscribe receive goroutine, serialized by the event-channel close.
	lastSubscribeErr error

	limits Limits

	// Two-phase auth state.
	playerSessionToken string                     // set after AuthenticatePlayer, persists across character selection
	characters         []*corev1.CharacterSummary // available characters while in selectMode
	selectMode         bool                       // true when waiting for PLAY/CREATE
	loggingOut         bool                       // true when LOGOUT initiated (close connection after quit)

	// sceneNudgeLast records the last time a SCENE_ACTIVITY nudge rendered for a
	// scene id, gating the per-scene debounce (D-02 throttle). Accessed only from
	// the single-consumer Handle event loop, so no lock is needed.
	sceneNudgeLast map[string]time.Time
}

// sceneNudgeWindow bounds how often a single scene's SCENE_ACTIVITY nudge
// renders on telnet: at most one [>GAME: …] line per window per scene id, so a
// busy scene cannot spam one line per pose (T-02-03).
const sceneNudgeWindow = 45 * time.Second

// NewGatewayHandler creates a new GatewayHandler for the given connection.
// limits bounds per-connection resource usage; callers SHOULD pass
// DefaultLimits unless they have a specific reason to deviate. The handler
// reads rendering metadata from EventFrame.Rendering on the wire and does
// not hold a local VerbRegistry (Phase 1.6 gateway thinness).
func NewGatewayHandler(conn net.Conn, client CoreClient, limits Limits) *GatewayHandler {
	dr := &deadlineReader{conn: conn, timeout: limits.IdleReadTimeout}
	return &GatewayHandler{
		conn:           conn,
		reader:         bufio.NewReader(dr),
		client:         client,
		limits:         limits,
		sceneNudgeLast: make(map[string]time.Time),
	}
}

// sceneActivityLine returns the throttled [>GAME: …] leader for a
// SCENE_ACTIVITY control frame, or "" when the scene was nudged within
// sceneNudgeWindow (per-scene debounce, D-02). It consumes only the scene id
// and reaches no scene service/store — the leader is structurally content-free
// (INV-SCENE-70). The event loop is single-consumer, so the debounce map needs
// no lock.
func (h *GatewayHandler) sceneActivityLine(sceneID string, now time.Time) string {
	if h.sceneNudgeLast == nil {
		h.sceneNudgeLast = make(map[string]time.Time)
	}
	if last, ok := h.sceneNudgeLast[sceneID]; ok && now.Sub(last) < sceneNudgeWindow {
		return ""
	}
	h.sceneNudgeLast[sceneID] = now
	return gamenotice.Activity(sceneID)
}

// Handle processes the connection until it is closed or the context is done.
func (h *GatewayHandler) Handle(ctx context.Context) {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	slog.DebugContext(
		ctx, "telnet: client connected",
		"remote_addr", h.conn.RemoteAddr().String(),
	)

	defer func() {
		slog.DebugContext(
			ctx, "telnet: client disconnected",
			"remote_addr", h.conn.RemoteAddr().String(),
			"session_id", h.sessionID,
		)
		if h.authed && h.sessionID != "" {
			// Use a fresh context — the session ctx may already be cancelled.
			disconnCtx, disconnCancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer disconnCancel()
			if _, err := h.client.Disconnect(disconnCtx, &corev1.DisconnectRequest{
				SessionId:          h.sessionID,
				ConnectionId:       h.connectionID,
				PlayerSessionToken: h.playerSessionToken,
			}); err != nil {
				slog.DebugContext(ctx, "gateway: disconnect RPC failed", "session_id", h.sessionID, "error", err)
			}
		}
		if err := h.conn.Close(); err != nil {
			slog.DebugContext(ctx, "gateway: error closing connection", "error", err)
		}
	}()

	h.send("Welcome to HoloMUSH!")
	h.send("Use: connect guest")

	preAuth := time.NewTimer(h.limits.PreAuthTimeout)
	defer preAuth.Stop()

	// refreshInterval guards against a zero-duration ticker (e.g., Limits{}
	// literals in tests that don't set LeaseRefreshInterval). A zero-duration
	// ticker panics; fall back to the DefaultLimits value.
	refreshInterval := h.limits.LeaseRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = DefaultLimits.LeaseRefreshInterval
	}
	refreshTicker := time.NewTicker(refreshInterval)
	defer refreshTicker.Stop()

	lineCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(h.reader)
		scanner.Buffer(make([]byte, 1024), maxLineSize)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			select {
			case lineCh <- line:
			case <-childCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				RecordIdleTimeout()
			}
			select {
			case errCh <- err:
			case <-childCtx.Done():
			}
		} else {
			select {
			case errCh <- io.EOF:
			case <-childCtx.Done():
			}
		}
	}()

	// eventRecv is nil until subscription is established, blocking the select case.
	var eventRecv <-chan *corev1.SubscribeResponse

	for {
		select {
		case <-childCtx.Done():
			return

		case <-refreshTicker.C:
			h.refreshOnce(childCtx)

		case <-preAuth.C:
			if !h.authed {
				h.send("Authentication timeout.")
				RecordPreAuthTimeout()
				return
			}
			// authed — timer fired benignly, fall through to next iteration.

		case err := <-errCh:
			if !errors.Is(err, io.EOF) {
				slog.DebugContext(childCtx, "gateway: connection read error", "error", err)
			}
			return

		case line := <-lineCh:
			if ch := h.processLine(childCtx, line); ch != nil {
				eventRecv = ch
			}
			if h.quitting {
				// Wait for the STREAM_CLOSED control frame with "Goodbye!" before
				// closing. The server sends it after processing quit. If the
				// server's Subscribe goroutine races the drain timer or exits
				// without emitting STREAM_CLOSED, fall back to "Goodbye!" so
				// the client's quit UX is deterministic.
				h.drainUntilClosed(childCtx, eventRecv, "Goodbye!")
				if h.loggingOut || h.playerSessionToken == "" {
					return // LOGOUT or guest: close connection
				}
				// QUIT: return to character picker
				eventRecv = nil
				h.sessionID = ""
				h.connectionID = ""
				h.charName = ""
				h.authed = false
				h.quitting = false
				h.refreshCharacterList(childCtx)
				h.selectMode = true
				h.showCharacterList()
				continue
			}

		case resp, ok := <-eventRecv:
			if !ok {
				eventRecv = nil
				if h.quitting || !h.authed || h.sessionID == "" {
					// Expected close during teardown / unauthenticated — not a
					// survival case.
					slog.DebugContext(childCtx, "gateway: event stream closed", "session_id", h.sessionID)
					continue
				}
				slog.DebugContext(childCtx, "gateway: core stream closed; attempting reconnect", "session_id", h.sessionID)
				h.send("[Reconnecting to server…]")
				// resubscribe is authoritative for terminal-vs-transient: it
				// probes each fresh stream's first frame and returns nil for a
				// reaped-session SESSION_NOT_FOUND (rsoe6.11.1) rather than
				// retrying to the ceiling.
				if ch := h.resubscribe(childCtx); ch != nil {
					eventRecv = ch
					h.send("[Reconnected.]")
				} else {
					// Reconnect abandoned (ceiling exceeded, ctx done, or the
					// session was reaped past its reattach TTL). If the player is
					// still authenticated at the gateway, return to the character
					// picker so they can re-select; otherwise the stream is lost.
					if h.playerSessionToken != "" {
						// rsoe6.11.2: release the (now-dead) core-side connection
						// before dropping our session state, so we don't leak a
						// session_connections row. The outer defer-Disconnect in
						// Handle only fires when Handle RETURNS; on this
						// return-to-picker path Handle keeps running, so without
						// this explicit call the old connection is never released.
						// Best-effort: a reaped session is already gone and a down
						// core can't be reached — log at Debug and proceed.
						discCtx, discCancel := context.WithTimeout(context.Background(), rpcTimeout)
						if _, discErr := h.client.Disconnect(discCtx, &corev1.DisconnectRequest{
							SessionId:          h.sessionID,
							ConnectionId:       h.connectionID,
							PlayerSessionToken: h.playerSessionToken,
						}); discErr != nil {
							slog.DebugContext(childCtx, "gateway: disconnect after abandoned reconnect failed", "session_id", h.sessionID, "error", discErr)
						}
						discCancel()
						h.send("Connection to server lost. Returning to character selection.")
						h.sessionID = ""
						h.connectionID = ""
						h.charName = ""
						h.authed = false
						h.refreshCharacterList(childCtx)
						h.selectMode = true
						h.showCharacterList()
					} else {
						h.send("Connection to server lost.")
					}
				}
				continue
			}
			switch frame := resp.GetFrame().(type) {
			case *corev1.SubscribeResponse_Event:
				// Single lastEventID (not a set) suffices: core acks JetStream
				// delivery AFTER send, so a durable resume replays at most the
				// one in-flight frame (holomush-rsoe6).
				if id := frame.Event.GetId(); id != "" {
					if id == h.lastEventID {
						continue // redelivery overlap after a reconnect — already shown
					}
					h.lastEventID = id
				}
				h.sendProtoEvent(frame.Event)
			case *corev1.SubscribeResponse_Control:
				switch frame.Control.GetSignal() {
				case corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED:
					if msg := frame.Control.GetMessage(); msg != "" {
						h.send(msg)
					}
					if h.playerSessionToken != "" {
						// Server-initiated disconnect: return to character picker.
						eventRecv = nil
						h.sessionID = ""
						h.connectionID = ""
						h.charName = ""
						h.authed = false
						h.refreshCharacterList(childCtx)
						h.selectMode = true
						h.showCharacterList()
						continue
					}
					return
				case corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY:
					// A non-focused member's scene has activity. Render a
					// throttled, content-free nudge — consume only the scene id;
					// never call a scene service/store or decrypt a payload
					// (INV-SCENE-70; T-02-01).
					if line := h.sceneActivityLine(frame.Control.GetSceneId(), time.Now()); line != "" {
						h.send(line)
					}
				default:
					// REPLAY_COMPLETE: no-op for telnet — replay renders the same as live.
					slog.DebugContext(childCtx, "gateway: replay complete", "session_id", h.sessionID)
				}
			}
		}
	}
}

func (h *GatewayHandler) processLine(ctx context.Context, line string) <-chan *corev1.SubscribeResponse {
	// In selectMode only PLAY, CREATE, and QUIT are accepted.
	if h.selectMode {
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "play "):
			return h.handlePlay(ctx, strings.TrimSpace(line[5:]))
		case strings.HasPrefix(lower, "create "):
			return h.handleCreate(ctx, strings.TrimSpace(line[7:]))
		case lower == "quit" || lower == "logout":
			h.handleLogout(ctx)
		default:
			h.send("Use PLAY <name|number> or CREATE <name>. Type QUIT to log out.")
		}
		return nil
	}

	cmd, arg := core.ParseCommand(line)

	switch cmd {
	case "connect":
		return h.handleConnect(ctx, arg)
	case "say":
		h.handleSay(ctx, arg)
	case "pose":
		h.handlePose(ctx, arg)
	case "quit":
		h.handleQuit(ctx)
	case "logout":
		h.handleLogout(ctx)
	case "disconnect":
		h.handleDisconnect(ctx)
	default:
		if cmd != "" {
			h.handleGenericCommand(ctx, cmd, arg)
		}
	}
	return nil
}

func (h *GatewayHandler) handleConnect(ctx context.Context, arg string) <-chan *corev1.SubscribeResponse {
	if h.authed {
		h.send("Already connected.")
		return nil
	}

	var username, password string
	parts := strings.SplitN(arg, " ", 2)
	username = parts[0]
	if len(parts) == 2 {
		password = parts[1]
	}

	if username == "" {
		h.send("Usage: connect <username> [password]")
		return nil
	}

	// Guest flow: use the CreateGuest RPC.
	if strings.EqualFold(username, "guest") {
		return h.handleConnectGuest(ctx)
	}

	// Registered player: two-phase flow.
	return h.handleConnectPlayer(ctx, username, password)
}

// handleConnectGuest creates an ephemeral guest player and auto-selects the character.
func (h *GatewayHandler) handleConnectGuest(ctx context.Context) <-chan *corev1.SubscribeResponse {
	createCtx, createCancel := context.WithTimeout(ctx, rpcTimeout)
	defer createCancel()

	resp, err := h.client.CreateGuest(createCtx, &corev1.CreateGuestRequest{})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: create guest RPC failed", "error", err)
		h.send("Guest login error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		h.send("Guest login failed: " + resp.GetErrorMessage())
		return nil
	}

	h.playerSessionToken = resp.GetPlayerSessionToken()
	h.characters = resp.GetCharacters()

	// Auto-select the single guest character.
	if len(h.characters) == 1 {
		return h.selectCharacter(ctx, h.characters[0])
	}

	// Should not happen for guests — CreateGuest always returns one character.
	slog.ErrorContext(ctx, "gateway: CreateGuest returned no characters")
	h.send("Guest login failed. Please try again.")
	return nil
}

// handleConnectPlayer handles the two-phase registered player login.
//
// SECURITY: The password arrives here in cleartext because telnet has no
// native encryption. Operators are expected to front the telnet listener
// with a TLS-terminating proxy (stunnel/haproxy) or recommend the web
// client for credential submission. See site/docs/operating/telnet-security.md.
func (h *GatewayHandler) handleConnectPlayer(ctx context.Context, username, password string) <-chan *corev1.SubscribeResponse {
	authCtx, authCancel := context.WithTimeout(ctx, rpcTimeout)
	defer authCancel()

	resp, err := h.client.AuthenticatePlayer(authCtx, &corev1.AuthenticatePlayerRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: authenticate player RPC failed", "error", err)
		h.send("Authentication error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		slog.DebugContext(
			ctx, "telnet: player authentication failed",
			"remote_addr", h.conn.RemoteAddr().String(),
		)
		h.send("Login failed. Use `connect guest` to play.")
		return nil
	}

	h.playerSessionToken = resp.GetPlayerSessionToken()
	h.characters = resp.GetCharacters()
	defaultID := resp.GetDefaultCharacterId()

	// Auto-select if exactly one character and it is the default.
	if len(h.characters) == 1 && defaultID == h.characters[0].GetCharacterId() {
		return h.selectCharacter(ctx, h.characters[0])
	}

	// Multiple characters — show list and enter selectMode.
	h.showCharacterList()
	h.selectMode = true
	return nil
}

// showCharacterList prints the character selection list to the client.
func (h *GatewayHandler) showCharacterList() {
	h.send("Your characters:")
	for i, ch := range h.characters {
		status := ""
		if ch.GetHasActiveSession() {
			status = " [active]"
		}
		h.send(fmt.Sprintf("  %d. %s%s", i+1, ch.GetCharacterName(), status))
	}
	h.send("Use PLAY <name|number> to select, or CREATE <name> for a new character.")
}

// handlePlay is called when the client sends PLAY <name|number> in selectMode.
func (h *GatewayHandler) handlePlay(ctx context.Context, arg string) <-chan *corev1.SubscribeResponse {
	if arg == "" {
		h.send("Usage: PLAY <name|number>")
		return nil
	}

	ch := h.resolveCharacter(arg)
	if ch == nil {
		h.send(fmt.Sprintf("No character matching %q. Use PLAY <name|number>.", arg))
		return nil
	}

	return h.selectCharacter(ctx, ch)
}

// handleCreate is called when the client sends CREATE <name> in selectMode.
func (h *GatewayHandler) handleCreate(ctx context.Context, name string) <-chan *corev1.SubscribeResponse {
	if name == "" {
		h.send("Usage: CREATE <name>")
		return nil
	}

	createCtx, createCancel := context.WithTimeout(ctx, rpcTimeout)
	defer createCancel()

	resp, err := h.client.CreateCharacter(createCtx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: h.playerSessionToken,
		CharacterName:      name,
	})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: create character RPC failed", "error", err)
		h.send("Character creation error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		h.send("Could not create character: " + resp.GetErrorMessage())
		return nil
	}

	// Auto-select the newly created character.
	newChar := &corev1.CharacterSummary{
		CharacterId:   resp.GetCharacterId(),
		CharacterName: resp.GetCharacterName(),
	}
	return h.selectCharacter(ctx, newChar)
}

// resolveCharacter looks up a character by 1-based index or case-insensitive name.
func (h *GatewayHandler) resolveCharacter(arg string) *corev1.CharacterSummary {
	// Try numeric index.
	if idx, err := strconv.Atoi(arg); err == nil {
		if idx >= 1 && idx <= len(h.characters) {
			return h.characters[idx-1]
		}
		return nil
	}

	// Try name match (case-insensitive).
	lower := strings.ToLower(arg)
	for _, ch := range h.characters {
		if strings.ToLower(ch.GetCharacterName()) == lower {
			return ch
		}
	}
	return nil
}

// selectCharacter calls SelectCharacter RPC and, on success, enters the game.
func (h *GatewayHandler) selectCharacter(ctx context.Context, ch *corev1.CharacterSummary) <-chan *corev1.SubscribeResponse {
	selCtx, selCancel := context.WithTimeout(ctx, rpcTimeout)
	defer selCancel()

	resp, err := h.client.SelectCharacter(selCtx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: h.playerSessionToken,
		CharacterId:        ch.GetCharacterId(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: select character RPC failed", "error", err)
		h.send("Character selection error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		h.send("Could not select character: " + resp.GetErrorMessage())
		return nil
	}

	h.sessionID = resp.GetSessionId()
	h.charName = resp.GetCharacterName()
	h.authed = true
	h.selectMode = false
	h.characters = nil

	slog.DebugContext(
		ctx, "telnet: authentication success",
		"session_id", h.sessionID,
		"character_name", h.charName,
	)

	if resp.GetReattached() {
		h.send("Reattaching to existing session...")
	}
	h.send(fmt.Sprintf("Welcome, %s!", h.charName))

	return h.subscribeAndEnter(ctx)
}

// subscribeAndEnter subscribes to events for the current session and returns
// the event channel. Called after successful auth (both guest and two-phase).
// Generates a per-connection connection_id and passes it to core's Subscribe
// RPC so core can register the connection in the session store (bd-j2xj).
// The same connection_id is reused for the deferred Disconnect on exit.
func (h *GatewayHandler) subscribeAndEnter(ctx context.Context) <-chan *corev1.SubscribeResponse {
	ch, err := h.trySubscribe(ctx)
	if err != nil {
		slog.WarnContext(ctx, "gateway: subscribe RPC failed — no live events", "session_id", h.sessionID, "error", err)
		return nil
	}
	return ch
}

// trySubscribe performs the Subscribe RPC for the current session and, on
// success, spawns the receive goroutine and returns the buffered event channel.
// Unlike subscribeAndEnter (which swallows the error to keep its nil-on-error
// contract for the auth callers), trySubscribe surfaces the Subscribe error so
// resubscribe can classify a terminal SESSION_NOT_FOUND apart from a transient
// core-down. A nil error with a non-nil channel means the subscription is live.
func (h *GatewayHandler) trySubscribe(ctx context.Context) (<-chan *corev1.SubscribeResponse, error) {
	h.connectionID = core.NewULID().String()
	stream, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
		SessionId:          h.sessionID,
		PlayerSessionToken: h.playerSessionToken,
		ConnectionId:       h.connectionID,
		ClientType:         "telnet",
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // pass the client.go-translated error through unwrapped so resubscribe can classify SESSION_NOT_FOUND (set by TranslateSubscribeErr in Client.Subscribe) vs RPC_FAILED; re-wrapping would mask the oops code.
	}
	if stream == nil {
		slog.WarnContext(ctx, "gateway: subscribe returned nil stream", "session_id", h.sessionID)
		return nil, nil
	}

	// Clear any error captured by a previous stream's receive goroutine before
	// arming this one. grpc-go's server-streaming dispatch returns
	// (nonNilStream, nil) from Subscribe even when the handler errors
	// immediately — the handler error (e.g. a reaped-session SESSION_NOT_FOUND,
	// stamped with a wire code by subscribeSessionNotFound) surfaces on the
	// FIRST Recv, NOT from the Subscribe call. So an immediate error does NOT
	// come back through this function's return; instead the goroutine below
	// classifies it through the same TranslateSubscribeErr the Client.Subscribe
	// wrapper uses and stashes it in h.lastSubscribeErr before closing the
	// channel. The Handle reconnect loop reads h.lastSubscribeErr after the
	// channel closes to decide terminal (reaped → picker) vs transient (retry)
	// — see the !ok branch in Handle (rsoe6.11.1).
	h.lastSubscribeErr = nil

	// Capture session ID and event channel as locals so the receive
	// goroutine never reads from h.* — the main Handle loop mutates those
	// fields during teardown (quit→character-picker reset), which would
	// race with concurrent reads here (holomush-umxj Mode A).
	eventCh := make(chan *corev1.SubscribeResponse, 16)
	sessionID := h.sessionID
	h.eventCh = eventCh

	go func() {
		defer close(eventCh)
		for {
			resp, recvErr := stream.Recv()
			if recvErr != nil {
				if !errors.Is(recvErr, io.EOF) {
					// Classify and stash for the Handle reconnect loop; the
					// channel close (deferred above) is the wakeup signal, this
					// field carries the terminal-vs-transient reason. Written
					// before close so a reader observing the closed channel sees
					// the populated field (happens-before via channel close).
					h.lastSubscribeErr = grpcclient.TranslateSubscribeErr(recvErr)
					slog.DebugContext(ctx, "gateway: event stream recv error", "session_id", sessionID, "error", recvErr)
				}
				return
			}
			select {
			case eventCh <- resp:
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventCh, nil
}

// subscribeErrIsTerminal reports whether err (as captured in h.lastSubscribeErr
// or returned by trySubscribe) is the terminal reaped-session signal:
// SESSION_NOT_FOUND. A terminal error means the session was reaped past its
// reattach TTL, so the gateway returns the client to the character picker rather
// than retrying. Any other error (or none) is transient → reconnect.
func subscribeErrIsTerminal(err error) bool {
	if err == nil {
		return false
	}
	var oe oops.OopsError
	return errors.As(err, &oe) && oe.Code() == "SESSION_NOT_FOUND"
}

// resubscribe re-establishes the core event subscription after a core-stream
// break, reusing the existing sessionID. The JetStream durable consumer resumes
// server-side (the single redelivery-overlap frame is deduped by lastEventID in
// the Handle loop). It retries with backoff until a per-outage deadline, so a
// brief core restart is survived while a genuinely-down core eventually gives up.
// Returns the new event channel, or nil if reconnection failed/was abandoned.
// SESSION_NOT_FOUND is terminal (returns nil): the session was reaped past its
// reattach TTL, so the caller returns the client to the character picker to
// re-select (telnet's in-process re-auth) rather than reattaching in-band.
func (h *GatewayHandler) resubscribe(ctx context.Context) <-chan *corev1.SubscribeResponse {
	ceiling := h.limits.LeaseRefreshInterval * 8 // ~2m at the 15s default; bounded per-outage
	if ceiling <= 0 {
		ceiling = 2 * time.Minute
	}
	deadline := time.Now().Add(ceiling)
	backoff := 100 * time.Millisecond
	for {
		// A reaped session surfaces in one of two shapes (rsoe6.11.1):
		//
		//   (1) The Subscribe RPC fails synchronously (older shape) — the
		//       Client.Subscribe wrapper runs TranslateSubscribeErr, so
		//       trySubscribe returns a SESSION_NOT_FOUND oops error directly.
		//
		//   (2) grpc-go returns (nonNilStream, nil) and defers the handler error
		//       to the first Recv (the production shape) — trySubscribe returns a
		//       live channel whose receive goroutine classifies the recv error
		//       via TranslateSubscribeErr, stashes it in h.lastSubscribeErr, and
		//       closes the channel. The Handle !ok branch then calls back into
		//       resubscribe, so on this iteration h.lastSubscribeErr (carried over
		//       from the just-closed stream) is the terminal signal.
		//
		// Checking the carried-over h.lastSubscribeErr at the top covers shape
		// (2); checking trySubscribe's direct return covers shape (1). Neither
		// loops to the reconnect ceiling.
		if subscribeErrIsTerminal(h.lastSubscribeErr) {
			slog.DebugContext(ctx, "gateway: session not found on resubscribe; returning to picker", "session_id", h.sessionID)
			return nil // terminal — caller returns to character picker
		}

		ch, err := h.trySubscribe(ctx)
		if err == nil && ch != nil {
			return ch
		}
		if err != nil {
			if subscribeErrIsTerminal(err) {
				slog.DebugContext(ctx, "gateway: session not found on resubscribe; returning to picker", "session_id", h.sessionID)
				return nil // terminal — caller returns to character picker
			}
			slog.DebugContext(ctx, "gateway: resubscribe attempt failed; will retry", "session_id", h.sessionID, "error", err)
		}
		if time.Now().After(deadline) {
			slog.WarnContext(ctx, "gateway: resubscribe exceeded reconnect ceiling", "session_id", h.sessionID)
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
	}
}

func (h *GatewayHandler) handleSay(ctx context.Context, message string) {
	if !h.authed {
		h.send("You must connect first.")
		return
	}
	if message == "" {
		h.send("Say what?")
		return
	}

	ctx, span := tracer.Start(
		ctx, "telnet.command",
		trace.WithAttributes(
			attribute.String("session.id", h.sessionID),
			attribute.String("character.name", h.charName),
			attribute.String("command", "say"),
		),
	)
	defer span.End()

	cmdCtx, cmdCancel := context.WithTimeout(ctx, rpcTimeout)
	defer cmdCancel()

	resp, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
		SessionId:          h.sessionID,
		Command:            "say " + message,
		PlayerSessionToken: h.playerSessionToken,
		ConnectionId:       h.connectionID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: say command failed", "session_id", h.sessionID, "error", err)
		h.send("Error: Your message could not be sent. Please try again.")
		return
	}
	if !resp.GetSuccess() {
		h.send("Error: " + resp.GetError())
		return
	}
	// Output comes via broadcast say event on the location stream.
}

func (h *GatewayHandler) handlePose(ctx context.Context, action string) {
	if !h.authed {
		h.send("You must connect first.")
		return
	}
	if action == "" {
		h.send("Pose what?")
		return
	}

	ctx, span := tracer.Start(
		ctx, "telnet.command",
		trace.WithAttributes(
			attribute.String("session.id", h.sessionID),
			attribute.String("character.name", h.charName),
			attribute.String("command", "pose"),
		),
	)
	defer span.End()

	cmdCtx, cmdCancel := context.WithTimeout(ctx, rpcTimeout)
	defer cmdCancel()

	resp, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
		SessionId:          h.sessionID,
		Command:            "pose " + action,
		PlayerSessionToken: h.playerSessionToken,
		ConnectionId:       h.connectionID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "gateway: pose command failed", "session_id", h.sessionID, "error", err)
		h.send("Error: Your action could not be sent. Please try again.")
		return
	}
	if !resp.GetSuccess() {
		h.send("Error: " + resp.GetError())
		return
	}
	// Output comes via broadcast pose event on the location stream.
}

func (h *GatewayHandler) handleGenericCommand(ctx context.Context, cmd, arg string) {
	if !h.authed {
		h.send("Unknown command: " + cmd)
		return
	}

	fullCmd := cmd
	if arg != "" {
		fullCmd = cmd + " " + arg
	}

	if len(fullCmd) > maxCommandSize {
		h.send("Command too long.")
		return
	}

	ctx, span := tracer.Start(
		ctx, "telnet.command",
		trace.WithAttributes(
			attribute.String("session.id", h.sessionID),
			attribute.String("character.name", h.charName),
			attribute.String("command", cmd),
		),
	)
	defer span.End()

	cmdCtx, cmdCancel := context.WithTimeout(ctx, rpcTimeout)
	defer cmdCancel()

	if _, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
		SessionId:          h.sessionID,
		Command:            fullCmd,
		PlayerSessionToken: h.playerSessionToken,
		ConnectionId:       h.connectionID,
	}); err != nil {
		slog.ErrorContext(ctx, "gateway: command failed", "session_id", h.sessionID, "command", cmd, "error", err)
		h.send("Error processing command.")
	}
	// Output (or error) comes via command_response event on the character stream.
}

// handleDisconnect closes this wire without ending the session. Other
// surfaces subscribed to the same session remain active.
func (h *GatewayHandler) handleDisconnect(ctx context.Context) {
	if !h.authed || h.sessionID == "" || h.connectionID == "" {
		h.send("You are not currently connected to a character.")
		return
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	_, err := h.client.Disconnect(rpcCtx, &corev1.DisconnectRequest{
		SessionId:          h.sessionID,
		ConnectionId:       h.connectionID,
		PlayerSessionToken: h.playerSessionToken,
	})
	if err != nil {
		slog.WarnContext(ctx, "gateway: disconnect RPC failed",
			"session_id", h.sessionID, "error", err)
	}

	h.send("Disconnected. Other surfaces remain active.")
	// Clear auth/session state so the deferred teardown in Handle does NOT
	// re-fire Disconnect for the connection we just removed. Keep quitting
	// and loggingOut true so the main loop exits (and skips the
	// character-picker branch) — the eventRecv channel will close naturally
	// when the server-side subscription for this connection tears down.
	h.authed = false
	h.sessionID = ""
	h.connectionID = ""
	h.quitting = true
	h.loggingOut = true // skip the "return to character picker" branch
}

// refreshOnce renews the server-side connection lease for the current
// session (holomush-rsoe6, I-LIVE-1). No-op unless authed with a session.
// Failure is transient — logged at Debug; the next tick retries and the
// lease sweep tolerates a missed refresh within LeaseTTL.
func (h *GatewayHandler) refreshOnce(ctx context.Context) {
	if !h.authed || h.sessionID == "" {
		return
	}
	// Detach into its own trace root so the periodic refresh does not orphan
	// the long-lived connection trace (holomush-m7djf).
	rCtx, rSpan := telemetry.DetachTrace(ctx, tracer, "gateway.lease_refresh")
	defer rSpan.End()
	rCtx, rCancel := context.WithTimeout(rCtx, rpcTimeout)
	defer rCancel()
	if _, err := h.client.RefreshConnection(rCtx, &corev1.RefreshConnectionRequest{
		SessionId:          h.sessionID,
		ConnectionId:       h.connectionID,
		PlayerSessionToken: h.playerSessionToken,
	}); err != nil {
		slog.DebugContext(rCtx, "gateway: lease refresh failed (transient)", "session_id", h.sessionID, "error", err)
	}
}

func (h *GatewayHandler) handleQuit(ctx context.Context) {
	if h.authed {
		// Forward quit to the server so it can emit events and clean up.
		spanCtx, span := tracer.Start(
			ctx, "telnet.command",
			trace.WithAttributes(
				attribute.String("session.id", h.sessionID),
				attribute.String("character.name", h.charName),
				attribute.String("command", "quit"),
			),
		)
		defer span.End()

		cmdCtx, cmdCancel := context.WithTimeout(spanCtx, rpcTimeout)
		defer cmdCancel()

		if _, err := h.client.HandleCommand(cmdCtx, &corev1.HandleCommandRequest{
			SessionId:          h.sessionID,
			Command:            "quit",
			PlayerSessionToken: h.playerSessionToken,
			ConnectionId:       h.connectionID,
		}); err != nil {
			slog.WarnContext(spanCtx, "gateway: quit command failed", "session_id", h.sessionID, "error", err)
		}
	}
	h.quitting = true
}

func (h *GatewayHandler) handleLogout(ctx context.Context) {
	if h.authed {
		// If playing a character, quit it first.
		h.handleQuit(ctx)
	}
	h.loggingOut = true
	if h.playerSessionToken != "" {
		logoutCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		if _, err := h.client.Logout(logoutCtx, &corev1.LogoutRequest{
			PlayerSessionToken: h.playerSessionToken,
		}); err != nil {
			slog.WarnContext(ctx, "gateway: logout RPC failed", "error", err)
		}
	}
	if !h.authed {
		// Not playing a character, just close.
		h.send("Goodbye!")
		h.quitting = true
	}
}

func (h *GatewayHandler) refreshCharacterList(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	resp, err := h.client.ListCharacters(listCtx, &corev1.ListCharactersRequest{
		PlayerSessionToken: h.playerSessionToken,
	})
	if err != nil {
		h.send("Failed to refresh character list.")
		return
	}
	h.characters = resp.GetCharacters()
}

// drainUntilClosed reads from the event channel until a STREAM_CLOSED
// control frame arrives or the channel closes or a timeout expires.
// Any event frames received are forwarded to the client. If the timeout
// fires or the channel closes without a message (e.g., server-side
// Subscribe goroutine exited on error before emitting STREAM_CLOSED),
// fallbackMsg is sent to the client so the quit/logout UX remains
// deterministic. Pass "" to disable the fallback.
//
// Why the fallback matters: the server emits STREAM_CLOSED with its
// "Goodbye!" message from a different goroutine than HandleCommand's
// Delete call — under CI load the Subscribe goroutine can race the
// 2s timer (observed with ~5% rate in
// test/integration/telnet/e2e_test.go "Lifecycle Events / emits leave
// event on guest disconnect"). Without the fallback, the telnet
// client sees the character-picker prompt but never receives
// "Goodbye!", which is the user-facing contract of `quit`.
func (h *GatewayHandler) drainUntilClosed(ctx context.Context, eventRecv <-chan *corev1.SubscribeResponse, fallbackMsg string) {
	if eventRecv == nil {
		// No event subscription to drain (e.g., selectMode logout). The
		// caller is responsible for emitting any user-facing message in
		// that path; a fallback here would duplicate it.
		return
	}
	timer := time.NewTimer(drainTimeout)
	defer timer.Stop()
	for {
		select {
		case resp, ok := <-eventRecv:
			if !ok {
				// Channel closed before STREAM_CLOSED — server goroutine
				// exited early. Deliver the fallback so the client sees
				// the quit/logout message it expects.
				if fallbackMsg != "" {
					h.send(fallbackMsg)
				}
				return
			}
			switch frame := resp.GetFrame().(type) {
			case *corev1.SubscribeResponse_Event:
				h.sendProtoEvent(frame.Event)
			case *corev1.SubscribeResponse_Control:
				if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
					switch msg := frame.Control.GetMessage(); {
					case msg != "":
						h.send(msg)
					case fallbackMsg != "":
						h.send(fallbackMsg)
					}
					return
				}
			}
		case <-timer.C:
			// Timed out waiting for STREAM_CLOSED. Log so operators can
			// see if this becomes common, and fall back to delivering
			// the expected message to the client.
			slog.WarnContext(
				ctx, "gateway: drain timed out waiting for STREAM_CLOSED",
				"session_id", h.sessionID,
				"fallback_msg", fallbackMsg,
			)
			if fallbackMsg != "" {
				h.send(fallbackMsg)
			}
			return
		}
	}
}

func (h *GatewayHandler) send(msg string) {
	if err := h.conn.SetWriteDeadline(time.Now().Add(h.limits.WriteTimeout)); err != nil {
		slog.Debug("gateway: failed to set write deadline", "error", err)
		return
	}
	if _, err := fmt.Fprintln(h.conn, sanitizeTelnetOutput(msg)); err != nil {
		slog.Debug("gateway: failed to send message", "error", err)
	}
}

func (h *GatewayHandler) sendProtoEvent(ev *corev1.EventFrame) {
	msg := h.formatEvent(ev)
	if msg != "" {
		h.send(msg)
	}
}

// formatEvent dispatches formatting by EventFrame.Rendering category+format.
// Returns empty string for events that should not be displayed in telnet.
//
// INV-EVENTBUS-6: events arriving without rendering metadata are dropped (return
// empty string) and counted via gatewaymetrics.DroppedNilRenderingTotal.
// A non-zero counter indicates the core process's RenderingPublisher
// failed to stamp rendering before publish, or a publisher path bypassed
// it. The gateway is a thin protocol-translation layer (Phase 1.6) and
// MUST NOT compute rendering metadata locally.
func (h *GatewayHandler) formatEvent(ev *corev1.EventFrame) string {
	rendering := ev.GetRendering()
	if rendering == nil {
		slog.Error(
			"telnet: dropping event with nil Rendering (INV-EVENTBUS-6)",
			"event_id", ev.GetId(),
			"event_type", ev.GetType(),
		)
		gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceTelnet, ev.GetType()).Inc()
		return ""
	}

	// Only format events targeted at TERMINAL or BOTH.
	// STATE-only and other non-terminal events have no telnet representation.
	target := rendering.GetDisplayTarget()
	if target != corev1.EventChannel_EVENT_CHANNEL_TERMINAL &&
		target != corev1.EventChannel_EVENT_CHANNEL_BOTH {
		return ""
	}

	// The idle nudge (review Finding 4) renders through the shared gamenotice.Idle
	// leader, NOT the generic system-notification path (which reads a "text"
	// payload the idle nudge does not carry) and NOT the SCENE_ACTIVITY "has new
	// activity" leader. Routed by event type before the category dispatch so the
	// dedicated `[>GAME: … is now idle]` phrasing is used. The scene_id is read
	// from the frame payload only — no scene service/store lookup (gateway-boundary).
	if ev.GetType() == sceneIdleNudgeType {
		return h.formatSceneIdleNudge(ev)
	}

	switch rendering.GetCategory() {
	case "communication":
		return h.formatCommunication(ev, rendering)
	case "movement":
		return h.formatMovement(ev, rendering)
	case "command":
		return h.formatCommand(ev, rendering)
	case "system":
		return h.formatSystem(ev)
	case "state":
		return "" // telnet has no sidebar
	default:
		return h.formatFallback(ev)
	}
}

// formatCommunication formats speech and action events.
// Speech: `<actor> <label>, "<text>"`
// Action: `<actor><space?><text>`
func (h *GatewayHandler) formatCommunication(ev *corev1.EventFrame, rendering *corev1.RenderingMetadata) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal communication payload", "type", ev.GetType(), "error", err)
		return ""
	}

	// actor_display_name and text are the CommunicationContent contract's
	// field names (holomush-kk1ot); the remaining keys are legacy. Both are
	// read so un-migrated emitters keep rendering while emitters migrate to
	// the new contract independently (kk1ot.7, kk1ot.9).
	actor := stringFromPayload(payload, "actor_display_name", "character_name", "sender_name")
	if actor == "" {
		actor = truncateActorID(ev.GetActorId())
	}

	switch rendering.GetFormat() {
	case "speech":
		text := stringFromPayload(payload, "text", "message")
		return fmt.Sprintf("%s %s, %q", actor, rendering.GetLabel(), text)
	case "action":
		text := stringFromPayload(payload, "text", "action", "notice", "message")
		noSpace, ok := payload["no_space"].(bool)
		if ok && noSpace {
			return fmt.Sprintf("%s%s", actor, text)
		}
		return fmt.Sprintf("%s %s", actor, text)
	default:
		text := stringFromPayload(payload, "text", "message", "action", "notice")
		return fmt.Sprintf("%s %s", actor, text)
	}
}

// formatMovement formats arrive/leave/move notifications.
func (h *GatewayHandler) formatMovement(ev *corev1.EventFrame, rendering *corev1.RenderingMetadata) string {
	_ = rendering // reserved for future format differentiation

	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal movement payload", "type", ev.GetType(), "error", err)
		return ""
	}

	actor := stringFromPayload(payload, "character_name")
	if actor == "" {
		actor = truncateActorID(ev.GetActorId())
	}

	switch ev.GetType() {
	case string(eventvocab.EventTypeArrive):
		return fmt.Sprintf("%s has arrived.", actor)
	case string(eventvocab.EventTypeLeave):
		reason := stringFromPayload(payload, "reason")
		if reason != "" {
			return fmt.Sprintf("%s has left (%s).", actor, reason)
		}
		return fmt.Sprintf("%s has left.", actor)
	default:
		return fmt.Sprintf("%s moves.", actor)
	}
}

// formatCommand formats narrative text and error messages.
func (h *GatewayHandler) formatCommand(ev *corev1.EventFrame, rendering *corev1.RenderingMetadata) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal command payload", "type", ev.GetType(), "error", err)
		return ""
	}

	text := stringFromPayload(payload, "text", "message")
	if rendering.GetFormat() == "error" {
		return fmt.Sprintf("[ERROR] %s", text)
	}
	return text
}

// sceneIdleNudgeType is the wire event type core-scenes emits when a scene goes
// idle (plugins/core-scenes/idle_scheduler.go). Rendered via gamenotice.Idle.
const sceneIdleNudgeType = "core-scenes:scene_idle_nudge"

// formatSceneIdleNudge renders a scene_idle_nudge EventFrame as the shared
// `[>GAME: Scene #<id> is now idle]` leader (gamenotice.Idle). It reads the
// scene_id from the frame payload only — the gateway performs no scene
// service/store lookup (gateway-boundary; the payload scene_id is authoritative).
func (h *GatewayHandler) formatSceneIdleNudge(ev *corev1.EventFrame) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal scene_idle_nudge payload", "type", ev.GetType(), "error", err)
		return ""
	}
	sceneID := stringFromPayload(payload, "scene_id")
	if sceneID == "" {
		slog.Warn("gateway: scene_idle_nudge missing scene_id", "type", ev.GetType())
		return ""
	}
	return gamenotice.Idle(sceneID)
}

// formatSystem formats system notification text.
func (h *GatewayHandler) formatSystem(ev *corev1.EventFrame) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal system payload", "type", ev.GetType(), "error", err)
		return ""
	}
	return stringFromPayload(payload, "text", "message")
}

// formatFallback handles unregistered event types.
func (h *GatewayHandler) formatFallback(ev *corev1.EventFrame) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Warn("gateway: unknown event type", "type", ev.GetType())
		return fmt.Sprintf("<event: %s>", ev.GetType())
	}
	text := stringFromPayload(payload, "text", "message")
	if text != "" {
		return text
	}
	slog.Warn("gateway: unknown event type with no text", "type", ev.GetType())
	return fmt.Sprintf("<event: %s>", ev.GetType())
}

// stringFromPayload returns the first non-empty string value found among the
// given keys in the payload map.
func stringFromPayload(payload map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := payload[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// truncateActorID returns a truncated actor ID for display when no character
// name is available.
func truncateActorID(actorID string) string {
	if len(actorID) > 8 {
		return actorID[:8]
	}
	return actorID
}
