package telnet

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

// ConnectionHandler handles a single telnet connection.
type ConnectionHandler struct {
	conn       net.Conn
	reader     *bufio.Reader
	engine     *core.Engine
	sessions   *core.SessionManager
	connID     ulid.ULID
	charID     ulid.ULID
	locationID ulid.ULID
	charName   string
	authed     bool
}

// NewConnectionHandler creates a new handler.
func NewConnectionHandler(conn net.Conn, engine *core.Engine, sessions *core.SessionManager) *ConnectionHandler {
	return &ConnectionHandler{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		engine:   engine,
		sessions: sessions,
		connID:   core.NewULID(),
	}
}

// Handle processes the connection until closed.
func (h *ConnectionHandler) Handle(ctx context.Context) {
	defer h.conn.Close()

	h.send("Welcome to HoloMUSH!")
	h.send("Use: connect <username> <password>")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := h.reader.ReadString('\n')
		if err != nil {
			if h.authed {
				h.sessions.Disconnect(h.charID, h.connID)
			}
			return
		}

		h.processLine(ctx, strings.TrimSpace(line))
	}
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

	// Hardcoded test character
	h.charID, _ = ulid.Parse("01JTEST001CHRCTR00000001")
	h.locationID, _ = ulid.Parse("01JTEST001LOCATN00000001")
	h.charName = "TestChar"
	h.authed = true

	h.sessions.Connect(h.charID, h.connID)
	h.send(fmt.Sprintf("Welcome back, %s!", h.charName))

	// Replay missed events
	stream := "location:" + h.locationID.String()
	events, _ := h.engine.ReplayEvents(ctx, h.charID, stream, 50)
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

	h.engine.HandleSay(ctx, h.charID, h.locationID, message)
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

	h.engine.HandlePose(ctx, h.charID, h.locationID, action)
}

func (h *ConnectionHandler) handleQuit() {
	h.send("Goodbye! Your session has been saved.")
	if h.authed {
		h.sessions.Disconnect(h.charID, h.connID)
	}
	h.conn.Close()
}

func (h *ConnectionHandler) send(msg string) {
	fmt.Fprintln(h.conn, msg)
}

func (h *ConnectionHandler) sendEvent(e core.Event) {
	switch e.Type {
	case core.EventTypeSay:
		var p core.SayPayload
		json.Unmarshal(e.Payload, &p)
		h.send(fmt.Sprintf("[%s] %s says, \"%s\"", e.Actor.ID[:8], h.charName, p.Message))
	case core.EventTypePose:
		var p core.PosePayload
		json.Unmarshal(e.Payload, &p)
		h.send(fmt.Sprintf("[%s] %s %s", e.Actor.ID[:8], h.charName, p.Action))
	}
}
