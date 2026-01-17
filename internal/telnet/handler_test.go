package telnet

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
)

// testConn wraps net.Pipe for testing.
type testConn struct {
	client net.Conn
	server net.Conn
	reader *bufio.Reader
	t      *testing.T
}

func newTestConn(t *testing.T) *testConn {
	t.Helper()
	client, server := net.Pipe()
	return &testConn{
		client: client,
		server: server,
		reader: bufio.NewReader(client),
		t:      t,
	}
}

func (tc *testConn) writeLine(s string) {
	tc.t.Helper()
	if err := tc.client.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		tc.t.Fatalf("failed to set write deadline: %v", err)
	}
	if _, err := tc.client.Write([]byte(s + "\n")); err != nil {
		tc.t.Fatalf("failed to write: %v", err)
	}
}

func (tc *testConn) readLine() string {
	tc.t.Helper()
	if err := tc.client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		tc.t.Fatalf("failed to set read deadline: %v", err)
	}
	line, err := tc.reader.ReadString('\n')
	if err != nil {
		tc.t.Fatalf("failed to read line: %v", err)
	}
	return strings.TrimSpace(line)
}

//nolint:unparam // n varies by test case needs
func (tc *testConn) readLines(n int) []string {
	tc.t.Helper()
	lines := make([]string, n)
	for i := range n {
		lines[i] = tc.readLine()
	}
	return lines
}

func (tc *testConn) close() {
	_ = tc.client.Close()
	_ = tc.server.Close()
}

//nolint:unparam // engine returned for future test extensibility
func newTestHandler(t *testing.T) (*ConnectionHandler, *testConn, *core.Engine) {
	t.Helper()
	tc := newTestConn(t)
	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	handler := NewConnectionHandler(tc.server, engine, sessions, broadcaster)
	return handler, tc, engine
}

// --- Connect command tests ---

func TestConnectionHandler_Connect_Success(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	// Read welcome messages
	tc.readLines(2) // "Welcome to HoloMUSH!" and "Use: connect..."

	// Connect with valid credentials
	tc.writeLine("connect testuser password")
	response := tc.readLine()

	if !strings.Contains(response, "Welcome back") {
		t.Errorf("expected welcome message, got: %s", response)
	}
	if !handler.authed {
		t.Error("expected handler to be authenticated")
	}
}

func TestConnectionHandler_Connect_AlreadyAuthed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	// First connect
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Try to connect again
	tc.writeLine("connect testuser password")
	response := tc.readLine()

	if !strings.Contains(response, "Already connected") {
		t.Errorf("expected 'Already connected', got: %s", response)
	}
}

func TestConnectionHandler_Connect_MissingPassword(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser")
	response := tc.readLine()

	if !strings.Contains(response, "Usage: connect") {
		t.Errorf("expected usage message, got: %s", response)
	}
}

func TestConnectionHandler_Connect_InvalidCredentials(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect wronguser wrongpass")
	response := tc.readLine()

	if !strings.Contains(response, "Invalid username or password") {
		t.Errorf("expected invalid credentials message, got: %s", response)
	}
}

// --- Look command tests ---

func TestConnectionHandler_Look_NotAuthed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("look")
	response := tc.readLine()

	if !strings.Contains(response, "must connect first") {
		t.Errorf("expected auth required message, got: %s", response)
	}
}

func TestConnectionHandler_Look_Success(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	// Connect first
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Look
	tc.writeLine("look")
	lines := tc.readLines(2) // Room name and description

	if lines[0] != "The Void" {
		t.Errorf("expected 'The Void', got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "empty expanse") {
		t.Errorf("expected room description, got: %s", lines[1])
	}
}

// --- Say command tests ---

func TestConnectionHandler_Say_NotAuthed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("say Hello!")
	response := tc.readLine()

	if !strings.Contains(response, "must connect first") {
		t.Errorf("expected auth required message, got: %s", response)
	}
}

func TestConnectionHandler_Say_EmptyMessage(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	tc.writeLine("say")
	response := tc.readLine()

	if !strings.Contains(response, "Say what?") {
		t.Errorf("expected 'Say what?', got: %s", response)
	}
}

func TestConnectionHandler_Say_Success(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	tc.writeLine("say Hello, world!")
	response := tc.readLine()

	if !strings.Contains(response, "You say") && !strings.Contains(response, "Hello, world!") {
		t.Errorf("expected say confirmation, got: %s", response)
	}
}

// --- Pose command tests ---

func TestConnectionHandler_Pose_NotAuthed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("pose waves")
	response := tc.readLine()

	if !strings.Contains(response, "must connect first") {
		t.Errorf("expected auth required message, got: %s", response)
	}
}

func TestConnectionHandler_Pose_EmptyAction(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	tc.writeLine("pose")
	response := tc.readLine()

	if !strings.Contains(response, "Pose what?") {
		t.Errorf("expected 'Pose what?', got: %s", response)
	}
}

func TestConnectionHandler_Pose_Success(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	tc.writeLine("pose waves happily")
	response := tc.readLine()

	if !strings.Contains(response, "TestChar") || !strings.Contains(response, "waves happily") {
		t.Errorf("expected pose confirmation with character name, got: %s", response)
	}
}

// --- Quit command tests ---

func TestConnectionHandler_Quit_NotAuthed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	tc.readLines(2) // Welcome messages

	tc.writeLine("quit")
	response := tc.readLine()

	if !strings.Contains(response, "Goodbye") {
		t.Errorf("expected 'Goodbye', got: %s", response)
	}

	// Wait for handler to exit
	select {
	case <-done:
		// Good, handler exited
	case <-time.After(time.Second):
		t.Error("handler did not exit after quit")
	}
}

func TestConnectionHandler_Quit_Authed(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	tc.readLines(2) // Welcome messages

	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	tc.writeLine("quit")
	response := tc.readLine()

	if !strings.Contains(response, "Goodbye") {
		t.Errorf("expected 'Goodbye', got: %s", response)
	}

	// Wait for handler to exit
	select {
	case <-done:
		// Good, handler exited
	case <-time.After(time.Second):
		t.Error("handler did not exit after quit")
	}
}

// --- Unknown command tests ---

func TestConnectionHandler_UnknownCommand(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	tc.writeLine("foobar")
	response := tc.readLine()

	if !strings.Contains(response, "Unknown command: foobar") {
		t.Errorf("expected unknown command message, got: %s", response)
	}
}

func TestConnectionHandler_EmptyLine(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	tc.readLines(2) // Welcome messages

	// Send empty line - should not produce any response
	tc.writeLine("")

	// Send a real command to verify handler is still working
	tc.writeLine("look")
	response := tc.readLine()

	if !strings.Contains(response, "must connect first") {
		t.Errorf("expected auth message after empty line, got: %s", response)
	}
}

// --- sendEvent tests ---

func TestConnectionHandler_SendEvent_Say(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	payload, _ := json.Marshal(core.SayPayload{Message: "Hello!"})
	event := core.Event{
		ID:      core.NewULID(),
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "01234567890123456789012345"},
		Payload: payload,
	}

	go func() {
		handler.sendEvent(event)
	}()

	response := tc.readLine()
	if !strings.Contains(response, "says") && !strings.Contains(response, "Hello!") {
		t.Errorf("expected say event format, got: %s", response)
	}
}

func TestConnectionHandler_SendEvent_Pose(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	payload, _ := json.Marshal(core.PosePayload{Action: "waves"})
	event := core.Event{
		ID:      core.NewULID(),
		Stream:  "location:test",
		Type:    core.EventTypePose,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "01234567890123456789012345"},
		Payload: payload,
	}

	go func() {
		handler.sendEvent(event)
	}()

	response := tc.readLine()
	if !strings.Contains(response, "waves") {
		t.Errorf("expected pose event format, got: %s", response)
	}
}

func TestConnectionHandler_SendEvent_CorruptedPayload(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	event := core.Event{
		ID:      core.NewULID(),
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "01234567"},
		Payload: []byte(`{invalid json`),
	}

	go func() {
		handler.sendEvent(event)
	}()

	response := tc.readLine()
	if !strings.Contains(response, "corrupted") {
		t.Errorf("expected corrupted message indicator, got: %s", response)
	}
}

func TestConnectionHandler_SendEvent_UnknownType(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	event := core.Event{
		ID:      core.NewULID(),
		Stream:  "location:test",
		Type:    core.EventType("unknown_type"),
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "01234567"},
		Payload: []byte(`{}`),
	}

	go func() {
		handler.sendEvent(event)
	}()

	response := tc.readLine()
	if !strings.Contains(response, "unknown_type") {
		t.Errorf("expected unknown type in output, got: %s", response)
	}
}

func TestConnectionHandler_SendEvent_ShortActorID(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	payload, _ := json.Marshal(core.SayPayload{Message: "Hi"})
	event := core.Event{
		ID:      core.NewULID(),
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "short"},
		Payload: payload,
	}

	go func() {
		handler.sendEvent(event)
	}()

	response := tc.readLine()
	// Should not panic and should use the full short ID
	if !strings.Contains(response, "short") {
		t.Errorf("expected short actor ID in output, got: %s", response)
	}
}

// --- Real-time event subscription tests ---

func TestConnectionHandler_ReceivesRealTimeEvents(t *testing.T) {
	tc := newTestConn(t)
	defer tc.close()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	handler := NewConnectionHandler(tc.server, engine, sessions, broadcaster)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	// Read welcome messages
	tc.readLines(2)

	// Connect
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Now another character says something (simulated by direct engine call)
	// This event should be broadcast and received by the connected handler
	otherCharID := core.NewULID()
	err := engine.HandleSay(ctx, otherCharID, testLocationID, "Hello from another player!")
	if err != nil {
		t.Fatalf("HandleSay failed: %v", err)
	}

	// The handler should receive and display the event via real-time broadcast
	// Keep reading until we find the expected message (there may be replay prefix)
	found := false
	for i := 0; i < 5; i++ {
		if err := tc.client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("failed to set deadline: %v", err)
		}
		response, err := tc.reader.ReadString('\n')
		if err != nil {
			break
		}
		response = strings.TrimSpace(response)
		if strings.Contains(response, "Hello from another player!") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to receive real-time event with 'Hello from another player!'")
	}
}

func TestConnectionHandler_ReceivesRealTimePoseEvents(t *testing.T) {
	tc := newTestConn(t)
	defer tc.close()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	handler := NewConnectionHandler(tc.server, engine, sessions, broadcaster)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	// Read welcome messages
	tc.readLines(2)

	// Connect
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Another character poses
	otherCharID := core.NewULID()
	err := engine.HandlePose(ctx, otherCharID, testLocationID, "waves hello")
	if err != nil {
		t.Fatalf("HandlePose failed: %v", err)
	}

	// The handler should receive and display the event via real-time broadcast
	// Keep reading until we find the expected message (there may be replay prefix)
	found := false
	for i := 0; i < 5; i++ {
		if err := tc.client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("failed to set deadline: %v", err)
		}
		response, err := tc.reader.ReadString('\n')
		if err != nil {
			break
		}
		response = strings.TrimSpace(response)
		if strings.Contains(response, "waves hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to receive real-time pose event with 'waves hello'")
	}
}

func TestConnectionHandler_UnsubscribesOnDisconnect(t *testing.T) {
	tc := newTestConn(t)

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	handler := NewConnectionHandler(tc.server, engine, sessions, broadcaster)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Read welcome messages
	tc.readLines(2)

	// Connect
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Quit
	tc.writeLine("quit")
	tc.readLine() // Goodbye

	// Wait for handler to exit
	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Fatal("handler did not exit")
	}

	tc.close()

	// After disconnect, broadcaster should not have the subscription
	// We verify this by checking that events don't cause issues
	// (the subscription channel should be cleaned up)
	otherCharID := core.NewULID()
	err := engine.HandleSay(ctx, otherCharID, testLocationID, "This should not cause issues")
	if err != nil {
		t.Fatalf("HandleSay after disconnect failed: %v", err)
	}
}

func TestConnectionHandler_FiltersOwnEvents(t *testing.T) {
	tc := newTestConn(t)
	defer tc.close()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	handler := NewConnectionHandler(tc.server, engine, sessions, broadcaster)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go handler.Handle(ctx)

	// Read welcome messages
	tc.readLines(2)

	// Connect
	tc.writeLine("connect testuser password")
	tc.readLine() // Welcome back

	// Say something - we should get "You say" confirmation, NOT the broadcast
	tc.writeLine("say Hello!")
	response := tc.readLine()

	// Should be the "You say" confirmation, not the broadcast format
	if !strings.Contains(response, "You say") {
		t.Errorf("expected 'You say' confirmation, got: %s", response)
	}

	// Verify we don't get a duplicate broadcast (try reading with short timeout)
	if err := tc.client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	_, err := tc.reader.ReadString('\n')
	if err == nil {
		t.Error("expected no additional message (own event should be filtered)")
	}
}

// --- Context cancellation test ---

func TestConnectionHandler_ContextCancellation(t *testing.T) {
	handler, tc, _ := newTestHandler(t)
	defer tc.close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	tc.readLines(2) // Welcome messages

	// Cancel context
	cancel()

	// Wait for handler to exit
	select {
	case <-done:
		// Good, handler exited on context cancellation
	case <-time.After(time.Second):
		t.Error("handler did not exit after context cancellation")
	}
}
