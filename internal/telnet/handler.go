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

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

// Hardcoded test data for Phase 1 - validated at package init time.
var (
	testCharID     ulid.ULID
	testLocationID ulid.ULID
)

func init() {
	var err error
	testCharID, err = ulid.Parse("01HK153X0006AFVGQT5ZYC0GEK")
	if err != nil {
		panic(fmt.Sprintf("invalid test character ULID constant: %v", err))
	}
	testLocationID, err = ulid.Parse("01HK153X0006AFVGQT61FPQX3S")
	if err != nil {
		panic(fmt.Sprintf("invalid test location ULID constant: %v", err))
	}
}

// ConnectionHandler handles a single telnet connection.
type ConnectionHandler struct {
	conn        net.Conn
	reader      *bufio.Reader
	engine      *core.Engine
	sessions    *core.SessionManager
	broadcaster *core.Broadcaster
	connID      ulid.ULID
	charID      ulid.ULID
	locationID  ulid.ULID
	charName    string
	authed      bool
	quitting    bool
	eventCh     chan core.Event
}

// NewConnectionHandler creates a new handler.
func NewConnectionHandler(conn net.Conn, engine *core.Engine, sessions *core.SessionManager, broadcaster *core.Broadcaster) *ConnectionHandler {
	return &ConnectionHandler{
		conn:        conn,
		reader:      bufio.NewReader(conn),
		engine:      engine,
		sessions:    sessions,
		broadcaster: broadcaster,
		connID:      core.NewULID(),
	}
}

// Handle processes the connection until closed.
func (h *ConnectionHandler) Handle(ctx context.Context) {
	defer func() {
		// Unsubscribe from events if subscribed
		if h.eventCh != nil && h.broadcaster != nil {
			stream := "location:" + h.locationID.String()
			h.broadcaster.Unsubscribe(stream, h.eventCh)
		}
		if err := h.conn.Close(); err != nil {
			slog.Debug("error closing connection", "error", err)
		}
	}()

	h.send("Welcome to HoloMUSH!")
	h.send("Use: connect <username> <password>")

	// Channel for lines read from the connection
	lineCh := make(chan string)
	errCh := make(chan error)

	go func() {
		for {
			line, err := h.reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- strings.TrimSpace(line)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case err := <-errCh:
			if !errors.Is(err, io.EOF) {
				slog.Debug("connection read error",
					"conn_id", h.connID.String(),
					"error", err,
				)
			}
			if h.authed {
				h.sessions.Disconnect(h.charID, h.connID)
			}
			return

		case line := <-lineCh:
			h.processLine(ctx, line)
			if h.quitting {
				return
			}

		case event := <-h.eventChanOrNil():
			// Filter out own events - don't show them via broadcast
			if event.Actor.ID != h.charID.String() {
				h.sendEvent(event)
			}
		}
	}
}

// eventChanOrNil returns the event channel if subscribed, or nil otherwise.
// Returning nil makes the select case block forever (never selected).
func (h *ConnectionHandler) eventChanOrNil() <-chan core.Event {
	if h.eventCh != nil {
		return h.eventCh
	}
	return nil
}

func (h *ConnectionHandler) processLine(ctx context.Context, line string) {
	cmd, arg := core.ParseCommand(line)

	switch cmd {
	case "connect":
		h.handleConnect(ctx, arg)
	case "look":
		h.handleLook(ctx)
	case "say":
		h.handleSay(ctx, arg)
	case "pose":
		h.handlePose(ctx, arg)
	case "quit":
		h.handleQuit()
	default:
		if cmd != "" {
			h.send("Unknown command: " + cmd)
		}
	}
}

func (h *ConnectionHandler) handleConnect(ctx context.Context, arg string) {
	if h.authed {
		h.send("Already connected.")
		return
	}

	parts := strings.SplitN(arg, " ", 2)
	if len(parts) != 2 {
		h.send("Usage: connect <username> <password>")
		return
	}

	// Hardcoded test user for Phase 1
	username, password := parts[0], parts[1]
	if username != "testuser" || password != "password" {
		h.send("Invalid username or password.")
		return
	}

	// Use package-level test data validated at init time
	h.charID = testCharID
	h.locationID = testLocationID
	h.charName = "TestChar"
	h.authed = true

	h.sessions.Connect(h.charID, h.connID)

	// Subscribe to location events for real-time updates
	if h.broadcaster != nil {
		stream := "location:" + h.locationID.String()
		h.eventCh = h.broadcaster.Subscribe(stream)
	}

	h.send(fmt.Sprintf("Welcome back, %s!", h.charName))

	// Replay missed events
	stream := "location:" + h.locationID.String()
	events, err := h.engine.ReplayEvents(ctx, h.charID, stream, 50)
	if err != nil {
		slog.Error("failed to replay events on connect",
			"char_id", h.charID.String(),
			"stream", stream,
			"error", err,
		)
		h.send("Warning: Could not retrieve missed events.")
	}
	if len(events) > 0 {
		h.send(fmt.Sprintf("--- %d missed events ---", len(events)))
		for _, e := range events {
			h.sendEvent(e)
		}
		h.send("--- end of replay ---")
	}
}

func (h *ConnectionHandler) handleLook(_ context.Context) {
	if !h.authed {
		h.send("You must connect first.")
		return
	}

	// Hardcoded for Phase 1
	h.send("The Void")
	h.send("An empty expanse of nothing. This is where it all begins.")
}

func (h *ConnectionHandler) handleSay(ctx context.Context, message string) {
	if !h.authed {
		h.send("You must connect first.")
		return
	}
	if message == "" {
		h.send("Say what?")
		return
	}

	if err := h.engine.HandleSay(ctx, h.charID, h.locationID, message); err != nil {
		slog.Error("failed to process say command",
			"char_id", h.charID.String(),
			"error", err,
		)
		h.send("Error: Your message could not be sent. Please try again.")
		return
	}
	h.send(fmt.Sprintf("You say, %q", message))
}

func (h *ConnectionHandler) handlePose(ctx context.Context, action string) {
	if !h.authed {
		h.send("You must connect first.")
		return
	}
	if action == "" {
		h.send("Pose what?")
		return
	}

	if err := h.engine.HandlePose(ctx, h.charID, h.locationID, action); err != nil {
		slog.Error("failed to process pose command",
			"char_id", h.charID.String(),
			"error", err,
		)
		h.send("Error: Your action could not be sent. Please try again.")
		return
	}
	h.send(fmt.Sprintf("%s %s", h.charName, action))
}

func (h *ConnectionHandler) handleQuit() {
	h.send("Goodbye!")
	if h.authed {
		h.sessions.Disconnect(h.charID, h.connID)
	}
	h.quitting = true
}

func (h *ConnectionHandler) send(msg string) {
	if _, err := fmt.Fprintln(h.conn, msg); err != nil {
		slog.Debug("failed to send message to client",
			"conn_id", h.connID.String(),
			"error", err,
		)
	}
}

func (h *ConnectionHandler) sendEvent(e core.Event) {
	// Safe substring for actor ID prefix
	actorPrefix := e.Actor.ID
	if len(actorPrefix) > 8 {
		actorPrefix = actorPrefix[:8]
	}

	switch e.Type {
	case core.EventTypeSay:
		var p core.SayPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			slog.Error("failed to unmarshal say event",
				"event_id", e.ID.String(),
				"stream", e.Stream,
				"error", err,
			)
			h.send(fmt.Sprintf("[%s] <corrupted message>", actorPrefix))
			return
		}
		// Note: Using actorPrefix as display name until character lookup is implemented
		h.send(fmt.Sprintf("[%s] %s says, %q", actorPrefix, actorPrefix, p.Message))
	case core.EventTypePose:
		var p core.PosePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			slog.Error("failed to unmarshal pose event",
				"event_id", e.ID.String(),
				"stream", e.Stream,
				"error", err,
			)
			h.send(fmt.Sprintf("[%s] <corrupted action>", actorPrefix))
			return
		}
		h.send(fmt.Sprintf("[%s] %s %s", actorPrefix, actorPrefix, p.Action))
	default:
		slog.Warn("unknown event type in sendEvent",
			"event_id", e.ID.String(),
			"type", e.Type,
		)
		h.send(fmt.Sprintf("[%s] <event: %s>", actorPrefix, e.Type))
	}
}
