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
	// defaultDrainTimeout is the default window used by drainEvents.
	defaultDrainTimeout = 500 * time.Millisecond
	// drainMaxEvents caps the number of events forwarded during drainEvents.
	drainMaxEvents = 10
)

// CoreClient is the gRPC interface used by GatewayHandler to communicate with
// the core service.
type CoreClient interface {
	Authenticate(ctx context.Context, req *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error)
	HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
	Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
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
	drainTimeout time.Duration
}

// NewGatewayHandler creates a new GatewayHandler for the given connection.
func NewGatewayHandler(conn net.Conn, client CoreClient) *GatewayHandler {
	return &GatewayHandler{
		conn:         conn,
		reader:       bufio.NewReader(conn),
		client:       client,
		drainTimeout: defaultDrainTimeout,
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
				// Drain pending events briefly so the "Goodbye!" command_response
				// (emitted by the quit RPC) reaches the client before we close.
				h.drainEvents(eventRecv)
				return
			}

		case ev, ok := <-eventRecv:
			if !ok {
				eventRecv = nil
				slog.Debug("gateway: event stream closed", "session_id", h.sessionID)
				h.send("Connection to server lost.")
				continue
			}
			h.sendProtoEvent(ev)
		}
	}
}

func (h *GatewayHandler) processLine(ctx context.Context, line string) <-chan *corev1.SubscribeResponse {
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

	// Subscribe to events for this session. The server defaults to the
	// session's location stream when no explicit streams are provided.
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
			ev, recvErr := stream.Recv()
			if recvErr != nil {
				if !errors.Is(recvErr, io.EOF) {
					slog.Debug("gateway: event stream recv error", "session_id", h.sessionID, "error", recvErr)
				}
				return
			}
			select {
			case h.eventCh <- ev:
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

// drainEvents reads from the event channel for a short window, forwarding any
// pending events (e.g. the "Goodbye!" command_response from quit) to the client
// before the connection closes. At most drainMaxEvents are forwarded.
func (h *GatewayHandler) drainEvents(eventRecv <-chan *corev1.SubscribeResponse) {
	if eventRecv == nil {
		return
	}
	timeout := h.drainTimeout
	if timeout <= 0 {
		timeout = defaultDrainTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for count := 0; count < drainMaxEvents; count++ {
		select {
		case ev, ok := <-eventRecv:
			if !ok {
				return
			}
			h.sendProtoEvent(ev)
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

func (h *GatewayHandler) sendProtoEvent(ev *corev1.SubscribeResponse) {
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
