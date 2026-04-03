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

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

var tracer = otel.Tracer("holomush.telnet")

const (
	// maxLineSize is the maximum line length from a telnet client (8 KiB).
	maxLineSize = 8 * 1024
	// maxCommandSize is the maximum length of a forwarded command (4 KiB).
	maxCommandSize = 4096
	// rpcTimeout is the per-call timeout for unary gRPC RPCs.
	rpcTimeout = 10 * time.Second
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
}

// GatewayHandler manages a single telnet connection, using gRPC to communicate
// with the core service.
type GatewayHandler struct {
	conn         net.Conn
	reader       *bufio.Reader
	client       CoreClient
	verbRegistry *core.VerbRegistry
	sessionID    string
	connectionID string
	charName     string
	authed       bool
	quitting     bool
	eventCh      chan *corev1.SubscribeResponse

	// Two-phase auth state.
	playerSessionToken string                     // set after AuthenticatePlayer, persists across character selection
	characters         []*corev1.CharacterSummary // available characters while in selectMode
	selectMode         bool                       // true when waiting for PLAY/CREATE
	loggingOut         bool                       // true when LOGOUT initiated (close connection after quit)
}

// NewGatewayHandler creates a new GatewayHandler for the given connection.
func NewGatewayHandler(conn net.Conn, client CoreClient, registry *core.VerbRegistry) *GatewayHandler {
	return &GatewayHandler{
		conn:         conn,
		reader:       bufio.NewReader(conn),
		client:       client,
		verbRegistry: registry,
	}
}

// Handle processes the connection until it is closed or the context is done.
func (h *GatewayHandler) Handle(ctx context.Context) {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	slog.DebugContext(ctx, "telnet: client connected",
		"remote_addr", h.conn.RemoteAddr().String(),
	)

	defer func() {
		slog.DebugContext(ctx, "telnet: client disconnected",
			"remote_addr", h.conn.RemoteAddr().String(),
			"session_id", h.sessionID,
		)
		if h.authed && h.sessionID != "" {
			// Use a fresh context — the session ctx may already be cancelled.
			disconnCtx, disconnCancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer disconnCancel()
			if _, err := h.client.Disconnect(disconnCtx, &corev1.DisconnectRequest{
				SessionId:    h.sessionID,
				ConnectionId: h.connectionID,
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
				// closing. The server sends it after processing quit.
				h.drainUntilClosed(eventRecv)
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
				slog.DebugContext(childCtx, "gateway: event stream closed", "session_id", h.sessionID)
				h.send("Connection to server lost.")
				continue
			}
			switch frame := resp.GetFrame().(type) {
			case *corev1.SubscribeResponse_Event:
				h.sendProtoEvent(frame.Event)
			case *corev1.SubscribeResponse_Control:
				if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
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
				}
				// REPLAY_COMPLETE: no-op for telnet — replay renders the same as live.
				slog.DebugContext(childCtx, "gateway: replay complete", "session_id", h.sessionID)
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
		slog.Error("gateway: create guest RPC failed", "error", err)
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
	slog.Error("gateway: CreateGuest returned no characters")
	h.send("Guest login failed. Please try again.")
	return nil
}

// handleConnectPlayer handles the two-phase registered player login.
func (h *GatewayHandler) handleConnectPlayer(ctx context.Context, username, password string) <-chan *corev1.SubscribeResponse {
	authCtx, authCancel := context.WithTimeout(ctx, rpcTimeout)
	defer authCancel()

	resp, err := h.client.AuthenticatePlayer(authCtx, &corev1.AuthenticatePlayerRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		slog.Error("gateway: authenticate player RPC failed", "error", err)
		h.send("Authentication error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		slog.DebugContext(ctx, "telnet: player authentication failed",
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
		slog.Error("gateway: create character RPC failed", "error", err)
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
		slog.Error("gateway: select character RPC failed", "error", err)
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

	slog.DebugContext(ctx, "telnet: authentication success",
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
func (h *GatewayHandler) subscribeAndEnter(ctx context.Context) <-chan *corev1.SubscribeResponse {
	stream, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
		SessionId: h.sessionID,
	})
	if err != nil {
		slog.Warn("gateway: subscribe RPC failed — no live events", "session_id", h.sessionID, "error", err)
		return nil
	}
	if stream == nil {
		slog.Warn("gateway: subscribe returned nil stream", "session_id", h.sessionID)
		return nil
	}

	h.eventCh = make(chan *corev1.SubscribeResponse, 16)

	go func() {
		defer close(h.eventCh)
		for {
			resp, recvErr := stream.Recv()
			if recvErr != nil {
				if !errors.Is(recvErr, io.EOF) {
					slog.DebugContext(ctx, "gateway: event stream recv error", "session_id", h.sessionID, "error", recvErr)
				}
				return
			}
			select {
			case h.eventCh <- resp:
			case <-ctx.Done():
				return
			}
		}
	}()

	return h.eventCh
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

	ctx, span := tracer.Start(ctx, "telnet.command",
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
		SessionId: h.sessionID,
		Command:   "say " + message,
	})
	if err != nil {
		slog.Error("gateway: say command failed", "session_id", h.sessionID, "error", err)
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

	ctx, span := tracer.Start(ctx, "telnet.command",
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
		SessionId: h.sessionID,
		Command:   "pose " + action,
	})
	if err != nil {
		slog.Error("gateway: pose command failed", "session_id", h.sessionID, "error", err)
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

	ctx, span := tracer.Start(ctx, "telnet.command",
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
		SessionId: h.sessionID,
		Command:   fullCmd,
	}); err != nil {
		slog.Error("gateway: command failed", "session_id", h.sessionID, "command", cmd, "error", err)
		h.send("Error processing command.")
	}
	// Output (or error) comes via command_response event on the character stream.
}

func (h *GatewayHandler) handleQuit(ctx context.Context) {
	if h.authed {
		// Forward quit to the server so it can emit events and clean up.
		spanCtx, span := tracer.Start(ctx, "telnet.command",
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
			SessionId: h.sessionID,
			Command:   "quit",
		}); err != nil {
			slog.Warn("gateway: quit command failed", "session_id", h.sessionID, "error", err)
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
			slog.Warn("gateway: logout RPC failed", "error", err)
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
// Any event frames received are forwarded to the client.
func (h *GatewayHandler) drainUntilClosed(eventRecv <-chan *corev1.SubscribeResponse) {
	if eventRecv == nil {
		return
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case resp, ok := <-eventRecv:
			if !ok {
				return
			}
			switch frame := resp.GetFrame().(type) {
			case *corev1.SubscribeResponse_Event:
				h.sendProtoEvent(frame.Event)
			case *corev1.SubscribeResponse_Control:
				if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
					if msg := frame.Control.GetMessage(); msg != "" {
						h.send(msg)
					}
					return
				}
			}
		case <-timer.C:
			return
		}
	}
}

func (h *GatewayHandler) send(msg string) {
	if _, err := fmt.Fprintln(h.conn, msg); err != nil {
		slog.Debug("gateway: failed to send message", "error", err)
	}
}

func (h *GatewayHandler) sendProtoEvent(ev *corev1.EventFrame) {
	msg := h.formatEvent(ev)
	if msg != "" {
		h.send(msg)
	}
}

// formatEvent uses the VerbRegistry to dispatch formatting by category+format.
// Returns empty string for events that should not be displayed in telnet.
func (h *GatewayHandler) formatEvent(ev *corev1.EventFrame) string {
	if h.verbRegistry == nil {
		return h.formatFallback(ev)
	}
	reg, found := h.verbRegistry.Lookup(ev.GetType())
	if !found {
		return h.formatFallback(ev)
	}

	// Only format events targeted at TERMINAL or BOTH.
	// STATE-only and other non-terminal events have no telnet representation.
	if reg.DisplayTarget != webv1.EventChannel_EVENT_CHANNEL_TERMINAL &&
		reg.DisplayTarget != webv1.EventChannel_EVENT_CHANNEL_BOTH {
		return ""
	}

	switch reg.Category {
	case "communication":
		return h.formatCommunication(ev, reg)
	case "movement":
		return h.formatMovement(ev, reg)
	case "command":
		return h.formatCommand(ev, reg)
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
func (h *GatewayHandler) formatCommunication(ev *corev1.EventFrame, reg core.VerbRegistration) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal communication payload", "type", ev.GetType(), "error", err)
		return ""
	}

	actor := stringFromPayload(payload, "character_name", "sender_name")
	if actor == "" {
		actor = truncateActorID(ev.GetActorId())
	}

	switch reg.Format {
	case "speech":
		text := stringFromPayload(payload, "message")
		return fmt.Sprintf("%s %s, %q", actor, reg.Label, text)
	case "action":
		text := stringFromPayload(payload, "action", "notice", "message")
		noSpace, ok := payload["no_space"].(bool)
		if ok && noSpace {
			return fmt.Sprintf("%s%s", actor, text)
		}
		return fmt.Sprintf("%s %s", actor, text)
	default:
		text := stringFromPayload(payload, "message", "action", "notice")
		return fmt.Sprintf("%s %s", actor, text)
	}
}

// formatMovement formats arrive/leave/move notifications.
func (h *GatewayHandler) formatMovement(ev *corev1.EventFrame, reg core.VerbRegistration) string {
	_ = reg // reserved for future format differentiation

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
	case string(core.EventTypeArrive):
		return fmt.Sprintf("%s has arrived.", actor)
	case string(core.EventTypeLeave):
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
func (h *GatewayHandler) formatCommand(ev *corev1.EventFrame, reg core.VerbRegistration) string {
	payload := make(map[string]any)
	if err := json.Unmarshal(ev.GetPayload(), &payload); err != nil {
		slog.Error("gateway: failed to unmarshal command payload", "type", ev.GetType(), "error", err)
		return ""
	}

	text := stringFromPayload(payload, "text", "message")
	if reg.Format == "error" {
		return fmt.Sprintf("[ERROR] %s", text)
	}
	return text
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
