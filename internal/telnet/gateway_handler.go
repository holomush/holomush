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

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

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

	// Two-phase auth state.
	playerToken string                   // set after AuthenticatePlayer, cleared after SelectCharacter
	characters  []*corev1.CharacterSummary // available characters while in selectMode
	selectMode  bool                     // true when waiting for PLAY/CREATE
}

// NewGatewayHandler creates a new GatewayHandler for the given connection.
func NewGatewayHandler(conn net.Conn, client CoreClient) *GatewayHandler {
	return &GatewayHandler{
		conn:   conn,
		reader: bufio.NewReader(conn),
		client: client,
	}
}

// Handle processes the connection until it is closed or the context is done.
func (h *GatewayHandler) Handle(ctx context.Context) {
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	defer func() {
		if h.authed && h.sessionID != "" {
			// Use a fresh context — the session ctx may already be cancelled.
			disconnCtx, disconnCancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer disconnCancel()
			if _, err := h.client.Disconnect(disconnCtx, &corev1.DisconnectRequest{
				SessionId:    h.sessionID,
				ConnectionId: h.connectionID,
			}); err != nil {
				slog.Debug("gateway: disconnect RPC failed", "session_id", h.sessionID, "error", err)
			}
		}
		if err := h.conn.Close(); err != nil {
			slog.Debug("gateway: error closing connection", "error", err)
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
				slog.Debug("gateway: connection read error", "error", err)
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
				return
			}

		case resp, ok := <-eventRecv:
			if !ok {
				eventRecv = nil
				slog.Debug("gateway: event stream closed", "session_id", h.sessionID)
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
					return
				}
				// REPLAY_COMPLETE: no-op for telnet — replay renders the same as live.
				slog.Debug("gateway: replay complete", "session_id", h.sessionID)
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
		case lower == "quit":
			h.handleQuit(ctx)
		default:
			h.send("Use PLAY <name|number> or CREATE <name>. Type QUIT to disconnect.")
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

	// Guest flow: use the legacy Authenticate RPC (unchanged).
	if strings.EqualFold(username, "guest") {
		return h.handleConnectGuest(ctx, username, password)
	}

	// Registered player: two-phase flow.
	return h.handleConnectPlayer(ctx, username, password)
}

// handleConnectGuest handles the legacy guest login path.
func (h *GatewayHandler) handleConnectGuest(ctx context.Context, username, password string) <-chan *corev1.SubscribeResponse {
	authCtx, authCancel := context.WithTimeout(ctx, rpcTimeout)
	defer authCancel()

	resp, err := h.client.Authenticate(authCtx, &corev1.AuthenticateRequest{
		Username:   username,
		Password:   password,
		ClientType: "telnet",
	})
	if err != nil {
		slog.Error("gateway: authenticate RPC failed", "error", err)
		h.send("Authentication error. Please try again.")
		return nil
	}
	if !resp.GetSuccess() {
		h.send("Login failed. Use `connect guest` to play.")
		return nil
	}

	h.sessionID = resp.GetSessionId()
	h.connectionID = resp.GetConnectionId()
	h.charName = resp.GetCharacterName()
	h.authed = true

	h.send(fmt.Sprintf("Welcome, %s!", h.charName))

	return h.subscribeAndEnter(ctx)
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
		h.send("Login failed. Use `connect guest` to play.")
		return nil
	}

	h.playerToken = resp.GetPlayerToken()
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
		PlayerToken:   h.playerToken,
		CharacterName: name,
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
		PlayerToken: h.playerToken,
		CharacterId: ch.GetCharacterId(),
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
	h.playerToken = ""
	h.characters = nil

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
					slog.Debug("gateway: event stream recv error", "session_id", h.sessionID, "error", recvErr)
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
		cmdCtx, cmdCancel := context.WithTimeout(ctx, rpcTimeout)
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
	actorID := ev.GetActorId()
	actorPrefix := actorID
	if len(actorPrefix) > 8 {
		actorPrefix = actorPrefix[:8]
	}

	switch ev.GetType() {
	case string(core.EventTypeSay):
		var p core.SayPayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("gateway: failed to unmarshal say event payload", "error", err)
			h.send(fmt.Sprintf("%s <corrupted message>", actorPrefix))
			return
		}
		h.send(fmt.Sprintf("%s says, %q", p.CharacterName, p.Message))

	case string(core.EventTypePose):
		var p core.PosePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("gateway: failed to unmarshal pose event payload", "error", err)
			h.send(fmt.Sprintf("%s <corrupted action>", actorPrefix))
			return
		}
		h.send(fmt.Sprintf("%s %s", p.CharacterName, p.Action))

	case string(core.EventTypeCommandResponse):
		var p core.CommandResponsePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("gateway: failed to unmarshal command_response payload", "error", err)
			return
		}
		h.send(p.Text)

	case string(core.EventTypeArrive):
		// Arrival notifications are persisted but not yet displayed to clients.
		slog.Debug("gateway: arrive event received", "session_id", h.sessionID)

	case string(core.EventTypeLeave):
		// Leave notifications are persisted but not yet displayed to clients.
		slog.Debug("gateway: leave event received", "session_id", h.sessionID)

	default:
		slog.Warn("gateway: unknown event type", "type", ev.GetType())
		h.send(fmt.Sprintf("<event: %s>", ev.GetType()))
	}
}
