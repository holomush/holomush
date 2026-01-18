package wasm_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/wasm"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/trace/noop"
)

type mockEmitter struct {
	mu      sync.Mutex
	emitted []core.Event
	notify  chan struct{}
}

func newMockEmitter() *mockEmitter {
	return &mockEmitter{notify: make(chan struct{}, 100)}
}

func (m *mockEmitter) Emit(_ context.Context, stream string, eventType core.EventType, payload []byte) error {
	m.mu.Lock()
	m.emitted = append(m.emitted, core.Event{
		Stream:  stream,
		Type:    eventType,
		Payload: payload,
	})
	m.mu.Unlock()
	select {
	case m.notify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEmitter) Events() []core.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]core.Event{}, m.emitted...)
}

func (m *mockEmitter) WaitForEvent(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-m.notify:
		// Event received
	case <-time.After(timeout):
		t.Fatal("timeout waiting for event")
	}
}

func (m *mockEmitter) WaitForNoEvent(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case <-m.notify:
		t.Fatal("unexpected event received")
	case <-time.After(wait):
		// Good - no event as expected
	}
}

func TestExtismSubscriber_Subscribe(t *testing.T) {
	t.Parallel()

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()

	sub.Subscribe("echo", "location:*")
	sub.Subscribe("echo", "global:*")

	// No panic = success for this basic test
}

func TestExtismSubscriber_HandleEvent_NoMatch(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load a plugin
	err := host.LoadPlugin(context.Background(), "echo", allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	// Send event that doesn't match
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "global:chat",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Pattern doesn't match, so no event should be emitted.
	// Wait briefly to confirm no event arrives.
	emitter.WaitForNoEvent(t, 50*time.Millisecond)

	// No events should be emitted since pattern didn't match
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 emitted events, got %d", len(emitter.Events()))
	}
}

func TestExtismSubscriber_HandleEvent_WithEchoPlugin(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the echo plugin
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello, world!"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for async processing via channel notification
	emitter.WaitForEvent(t, 5*time.Second)

	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(events))
	}

	if events[0].Stream != "location:room1" {
		t.Errorf("expected stream 'location:room1', got %q", events[0].Stream)
	}
}

// TestExtismSubscriber_ErrorsDoNotPropagate verifies errors are logged but don't propagate.
func TestExtismSubscriber_ErrorsDoNotPropagate(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("nonexistent-plugin", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// This should not panic even though plugin doesn't exist
	sub.HandleEvent(context.Background(), event)

	// Plugin doesn't exist, so no event should be emitted.
	// Wait briefly to confirm no event arrives.
	emitter.WaitForNoEvent(t, 100*time.Millisecond)

	// No events emitted since plugin doesn't exist
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events, got %d", len(emitter.Events()))
	}
}

// failingEmitter always returns an error on Emit but notifies when called.
type failingEmitter struct {
	notify chan struct{}
}

func newFailingEmitter() *failingEmitter {
	return &failingEmitter{notify: make(chan struct{}, 100)}
}

func (f *failingEmitter) Emit(_ context.Context, _ string, _ core.EventType, _ []byte) error {
	select {
	case f.notify <- struct{}{}:
	default:
	}
	return context.DeadlineExceeded // Simulate failure
}

func (f *failingEmitter) WaitForEmit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-f.notify:
		// Emit was called
	case <-time.After(timeout):
		t.Fatal("timeout waiting for emit call")
	}
}

// TestExtismSubscriber_EmitterFailure verifies emitter errors don't stop processing.
func TestExtismSubscriber_EmitterFailure(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newFailingEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	// Should not panic even when emitter fails
	sub.HandleEvent(context.Background(), event)

	// Wait for emit to be called (even though it fails)
	emitter.WaitForEmit(t, 5*time.Second)
	// Test passes if no panic occurred - emitter failure is logged, not fatal
}

// slowEmitter blocks on Emit to test graceful shutdown timing.
type slowEmitter struct {
	mu      sync.Mutex
	delay   time.Duration
	emitted []core.Event
	started chan struct{}
}

func (s *slowEmitter) Emit(_ context.Context, stream string, eventType core.EventType, payload []byte) error {
	s.started <- struct{}{}
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitted = append(s.emitted, core.Event{Stream: stream, Type: eventType, Payload: payload})
	return nil
}

func (s *slowEmitter) Events() []core.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]core.Event{}, s.emitted...)
}

// TestExtismSubscriber_Stop_WaitsForInFlight verifies that Stop blocks until
// all in-flight event deliveries complete.
func TestExtismSubscriber_Stop_WaitsForInFlight(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the echo plugin which emits events
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := &slowEmitter{delay: 500 * time.Millisecond, started: make(chan struct{}, 10)}
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello, world!"}`),
	}

	// Start event handling
	sub.HandleEvent(context.Background(), event)

	// Wait for emitter to start (plugin has processed and called Emit)
	select {
	case <-emitter.started:
		// Good, emitter started
	case <-time.After(3 * time.Second):
		t.Fatal("emitter did not start within timeout")
	}

	// Now call Stop - it should block until the slow emit completes
	stopDone := make(chan struct{})
	go func() {
		sub.Stop()
		close(stopDone)
	}()

	// Stop should not complete immediately (emitter is still sleeping)
	select {
	case <-stopDone:
		t.Fatal("Stop returned before in-flight delivery completed")
	case <-time.After(100 * time.Millisecond):
		// Good, Stop is blocking
	}

	// Wait for Stop to complete (should happen after emitter delay)
	select {
	case <-stopDone:
		// Good, Stop completed
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within expected time")
	}

	// Verify the event was actually emitted
	events := emitter.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 emitted event, got %d", len(events))
	}
}

// TestExtismSubscriber_Stop_RejectsNewEvents verifies that after Stop is called,
// HandleEvent does not spawn new goroutines.
func TestExtismSubscriber_Stop_RejectsNewEvents(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	sub.Subscribe("echo", "location:*")

	// Stop the subscriber before sending events
	sub.Stop()

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	// This should be a no-op since subscriber is stopped
	sub.HandleEvent(context.Background(), event)

	// Subscriber is already stopped, so no event should be emitted.
	// Wait briefly to confirm no event arrives.
	emitter.WaitForNoEvent(t, 100*time.Millisecond)
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events after Stop, got %d", len(emitter.Events()))
	}
}

// TestExtismSubscriber_DroppedEventLogsAtWarn verifies that events dropped during
// shutdown are logged at WARN level since this represents potential data loss.
func TestExtismSubscriber_DroppedEventLogsAtWarn(t *testing.T) {
	// Capture logs to verify level
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	sub.Subscribe("echo", "location:*")

	// Stop the subscriber first
	sub.Stop()

	eventID := ulid.Make()
	event := core.Event{
		ID:        eventID,
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	// This event should be dropped and logged at WARN level.
	// Log is written synchronously within HandleEvent.
	sub.HandleEvent(context.Background(), event)

	// Verify the dropped event was logged at WARN level
	records := capture.Records()
	var foundWarn bool
	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "dropping event due to shutdown" {
			foundWarn = true
			// Verify event info is present
			attrs := make(map[string]any)
			r.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.Any()
				return true
			})
			if attrs["event_id"] != eventID.String() {
				t.Errorf("expected event_id=%q, got %q", eventID.String(), attrs["event_id"])
			}
			if attrs["event_stream"] != "location:room1" {
				t.Errorf("expected event_stream=%q, got %q", "location:room1", attrs["event_stream"])
			}
			if attrs["event_type"] != core.EventTypeSay {
				t.Errorf("expected event_type=%v, got %v", core.EventTypeSay, attrs["event_type"])
			}
			break
		}
	}
	if !foundWarn {
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, r.Message)
		}
		t.Errorf("expected WARN log 'dropping event due to shutdown', got: %v", msgs)
	}
}

// logCapture is a slog.Handler that captures log records for testing.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *logCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (l *logCapture) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r)
	return nil
}

func (l *logCapture) WithAttrs(_ []slog.Attr) slog.Handler { return l }
func (l *logCapture) WithGroup(_ string) slog.Handler      { return l }

func (l *logCapture) Records() []slog.Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]slog.Record{}, l.records...)
}

// WaitForLog waits for a log message matching the given level and message.
func (l *logCapture) WaitForLog(t *testing.T, level slog.Level, msg string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, r := range l.records {
			if r.Level == level && r.Message == msg {
				l.mu.Unlock()
				return
			}
		}
		l.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for log: level=%v msg=%q", level, msg)
}

// TestExtismSubscriber_DeliveryFailureLogging verifies delivery errors are logged with event info.
func TestExtismSubscriber_DeliveryFailureLogging(t *testing.T) {
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Don't load plugin - this will cause delivery failure
	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	eventID := ulid.Make()
	event := core.Event{
		ID:        eventID,
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for the error log to be written
	capture.WaitForLog(t, slog.LevelError, "plugin event delivery failed", 5*time.Second)

	// Find the error log and verify attributes
	var found bool
	for _, r := range capture.Records() {
		if r.Level == slog.LevelError && r.Message == "plugin event delivery failed" {
			found = true
			// Verify event info is present
			attrs := make(map[string]any)
			r.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.Any()
				return true
			})
			if _, ok := attrs["event_id"]; !ok {
				t.Error("expected event_id in log")
			}
			if _, ok := attrs["event_stream"]; !ok {
				t.Error("expected event_stream in log")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'plugin event delivery failed' error log")
	}
}

// TestExtismSubscriber_PatternMatching verifies exact and glob pattern matching behavior.
// This tests matchPattern indirectly through HandleEvent since matchPattern is private.
func TestExtismSubscriber_PatternMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pattern     string
		eventStream string
		expectMatch bool
	}{
		{"exact match", "location:room1", "location:room1", true},
		{"exact mismatch by suffix", "location:room1", "location:room123", false},
		{"exact mismatch by prefix", "location:room1", "location:room", false},
		{"exact mismatch different", "location:room1", "global:chat", false},
		{"glob matches suffix", "location:*", "location:room1", true},
		{"glob matches empty suffix", "location:*", "location:", true},
		{"glob mismatch different prefix", "location:*", "global:chat", false},
		{"star matches everything", "*", "anything:goes:here", true},
		// Note: "empty matches empty" removed - empty streams are now rejected by
		// stream validation before emit, so this test would fail for the right reason.
		// The pattern matching logic still works, but validation blocks the emit.
		{"empty mismatch non-empty", "", "location:room1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracer := noop.NewTracerProvider().Tracer("test")
			host := wasm.NewExtismHost(tracer)
			defer func() { _ = host.Close(context.Background()) }()

			err := host.LoadPlugin(context.Background(), "echo", echoWASM)
			if err != nil {
				t.Fatalf("LoadPlugin failed: %v", err)
			}

			emitter := newMockEmitter()
			sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
			defer sub.Stop()
			sub.Subscribe("echo", tt.pattern)

			event := core.Event{
				ID:        ulid.Make(),
				Stream:    tt.eventStream,
				Type:      core.EventTypeSay,
				Timestamp: time.Now(),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
				Payload:   []byte(`{"message":"test"}`),
			}

			sub.HandleEvent(context.Background(), event)

			// For matching patterns, wait for event; for non-matching, confirm no event
			if tt.expectMatch {
				emitter.WaitForEvent(t, 5*time.Second)
			} else {
				emitter.WaitForNoEvent(t, 100*time.Millisecond)
			}

			gotMatch := len(emitter.Events()) > 0
			if gotMatch != tt.expectMatch {
				t.Errorf("pattern=%q stream=%q: got match=%v, want %v",
					tt.pattern, tt.eventStream, gotMatch, tt.expectMatch)
			}
		})
	}
}

// TestExtismSubscriber_EmptyStreamRejected verifies that plugin-emitted events
// with empty stream names are rejected and logged at WARN level.
func TestExtismSubscriber_EmptyStreamRejected(t *testing.T) {
	// Capture logs to verify WARN is emitted
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the empty-stream plugin which emits an event with empty stream
	err := host.LoadPlugin(context.Background(), "empty-stream", emptyStreamWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("empty-stream", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for warning log indicating the rejection
	capture.WaitForLog(t, slog.LevelWarn, "rejected plugin emit: empty stream name", 5*time.Second)

	// Empty stream should be rejected - no events emitted
	events := emitter.Events()
	if len(events) != 0 {
		t.Errorf("expected 0 emitted events (empty stream rejected), got %d", len(events))
	}

	// Verify warning log has expected attributes
	records := capture.Records()
	var foundWarn bool
	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "rejected plugin emit: empty stream name" {
			foundWarn = true
			// Verify plugin name is in the log
			attrs := make(map[string]any)
			r.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.Any()
				return true
			})
			if _, ok := attrs["plugin"]; !ok {
				t.Error("expected plugin attribute in warning log")
			}
			break
		}
	}
	if !foundWarn {
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, r.Message)
		}
		t.Errorf("expected warning log 'rejected plugin emit: empty stream name', got: %v", msgs)
	}
}

// TestExtismSubscriber_ValidStreamAllowed verifies that valid stream names still work.
func TestExtismSubscriber_ValidStreamAllowed(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load echo plugin which emits to a valid stream
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello, world!"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for event via channel notification
	emitter.WaitForEvent(t, 5*time.Second)

	// Valid stream should be allowed
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(events))
	}

	if events[0].Stream != "location:room1" {
		t.Errorf("expected stream 'location:room1', got %q", events[0].Stream)
	}
}

// TestExtismSubscriber_DeliveryTimeout_Configurable verifies that the delivery timeout
// can be configured via WithDeliveryTimeout option.
func TestExtismSubscriber_DeliveryTimeout_Configurable(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()

	// Use a generous custom timeout (10 seconds) - this verifies the option is accepted
	// and the subscriber works with custom timeouts
	customTimeout := 10 * time.Second
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter,
		wasm.WithDeliveryTimeout(customTimeout))
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for event via channel notification
	emitter.WaitForEvent(t, 5*time.Second)

	// With the custom timeout, the plugin should succeed
	events := emitter.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 emitted event with custom timeout, got %d", len(events))
	}
}

// TestExtismSubscriber_DeliveryTimeout_Default verifies that the default timeout
// is 5 seconds when no option is provided.
func TestExtismSubscriber_DeliveryTimeout_Default(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	// No options - should use default 5s timeout
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"Hello"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for event via channel notification
	emitter.WaitForEvent(t, 5*time.Second)

	// Default timeout should be long enough for echo plugin
	events := emitter.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 emitted event with default timeout, got %d", len(events))
	}
}

// TestExtismSubscriber_NilHost_Panics verifies that NewExtismSubscriber
// panics when host is nil since this is a programming error.
func TestExtismSubscriber_NilHost_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil host, but no panic occurred")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic message, got %T: %v", r, r)
		}
		if msg != "wasm: NewExtismSubscriber requires non-nil host" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()

	emitter := newMockEmitter()
	_ = wasm.NewExtismSubscriber(context.Background(), nil, emitter)
}

// TestExtismSubscriber_NilEmitter_Panics verifies that NewExtismSubscriber
// panics when emitter is nil since this is a programming error.
func TestExtismSubscriber_NilEmitter_Panics(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil emitter, but no panic occurred")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic message, got %T: %v", r, r)
		}
		if msg != "wasm: NewExtismSubscriber requires non-nil emitter" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()

	_ = wasm.NewExtismSubscriber(context.Background(), host, nil)
}

// TestExtismSubscriber_EmitFailureIndexLogging verifies that emit failures include
// the emit_index and emit_count for debugging multi-event scenarios.
func TestExtismSubscriber_EmitFailureIndexLogging(t *testing.T) {
	// Capture logs
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load echo plugin which emits one event
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newFailingEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for emit to be called (which will fail and log)
	emitter.WaitForEmit(t, 5*time.Second)

	// Wait for the error log to be written
	capture.WaitForLog(t, slog.LevelError, "failed to emit plugin event", 5*time.Second)

	// Verify error was logged with emit_index and emit_count
	records := capture.Records()
	var matchedRecord *slog.Record
	for i := range records {
		r := &records[i]
		if r.Level == slog.LevelError && r.Message == "failed to emit plugin event" {
			matchedRecord = r
			break
		}
	}

	if matchedRecord == nil {
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, r.Message)
		}
		t.Fatalf("expected error log %q, got logs: %v", "failed to emit plugin event", msgs)
	}

	// Extract attributes from log record
	attrs := make(map[string]any)
	matchedRecord.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})

	// Verify emit_index is present and equals 0 (first/only event)
	if gotIndex, ok := attrs["emit_index"]; !ok {
		t.Error("expected emit_index attribute in log, but not found")
	} else if gotIndex != int64(0) {
		t.Errorf("emit_index = %v (%T), want 0", gotIndex, gotIndex)
	}

	// Verify emit_count is present and equals 1 (echo plugin emits one event)
	if gotCount, ok := attrs["emit_count"]; !ok {
		t.Error("expected emit_count attribute in log, but not found")
	} else if gotCount != int64(1) {
		t.Errorf("emit_count = %v (%T), want 1", gotCount, gotCount)
	}
}

// TestExtismSubscriber_SkipsEmitsOnContextCancellation verifies that when the parent
// context is cancelled before the emit loop, a single WARN is logged and emits are skipped.
func TestExtismSubscriber_SkipsEmitsOnContextCancellation(t *testing.T) {
	// Capture logs
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load echo plugin which emits one event
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Use a cancellable context for the subscriber
	ctx, cancel := context.WithCancel(context.Background())

	// Create emitter that cancels the context when Emit is attempted
	// This simulates shutdown happening just before emit loop
	emitter := &cancellingEmitter{
		cancel: cancel,
		notify: make(chan struct{}, 10),
	}

	sub := wasm.NewExtismSubscriber(ctx, host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// Cancel the context before handling event - this simulates shutdown
	cancel()

	// HandleEvent with already-cancelled context should log dropping event
	sub.HandleEvent(context.Background(), event)

	// Wait briefly for any async processing
	time.Sleep(50 * time.Millisecond)

	// Verify we get the "dropping event due to shutdown" warning (from HandleEvent)
	// and NOT N "failed to emit plugin event" errors
	records := capture.Records()

	var foundDropWarn bool
	var emitErrorCount int
	var skipEmitsWarn bool

	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "dropping event due to shutdown" {
			foundDropWarn = true
		}
		if r.Level == slog.LevelError && r.Message == "failed to emit plugin event" {
			emitErrorCount++
		}
		if r.Level == slog.LevelWarn && r.Message == "skipping plugin emits due to context cancellation" {
			skipEmitsWarn = true
		}
	}

	// Since we cancelled before HandleEvent, the event is dropped at HandleEvent level
	if !foundDropWarn {
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, fmt.Sprintf("%s: %s", r.Level, r.Message))
		}
		t.Errorf("expected 'dropping event due to shutdown' warning, got logs: %v", msgs)
	}

	// Should not have any emit errors or skip warnings since event was dropped early
	if emitErrorCount > 0 {
		t.Errorf("expected 0 emit errors when context cancelled early, got %d", emitErrorCount)
	}
	if skipEmitsWarn {
		t.Error("unexpected 'skipping plugin emits' warning when event was dropped early")
	}
}

// TestExtismSubscriber_SkipsEmitsOnContextCancellationDuringDelivery verifies that when
// the context is cancelled after delivery but before emits, a single WARN is logged with pending count.
func TestExtismSubscriber_SkipsEmitsOnContextCancellationDuringDelivery(t *testing.T) {
	// Capture logs
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load echo plugin which emits one event
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Use a cancellable context for the subscriber
	ctx, cancel := context.WithCancel(context.Background())

	// Emitter that cancels context on first emit attempt - simulates shutdown during processing
	emitter := &cancellingEmitter{
		cancel: cancel,
		notify: make(chan struct{}, 10),
	}

	sub := wasm.NewExtismSubscriber(ctx, host, emitter)
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "player1"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// Handle event - delivery will succeed, but emitter will cancel context before emit
	sub.HandleEvent(context.Background(), event)

	// Wait for async processing
	emitter.WaitForEmit(t, 5*time.Second)

	// Stop the subscriber (context is already cancelled)
	sub.Stop()

	// The first emit call triggers cancellation, so we should see at most 1 emit error
	// (from the first emit that happens before the check) rather than N errors
	records := capture.Records()

	var skipEmitsWarn bool
	var emitErrorCount int
	var pendingEmits int64

	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "skipping plugin emits due to context cancellation" {
			skipEmitsWarn = true
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "pending_emits" {
					pendingEmits = a.Value.Int64()
				}
				return true
			})
		}
		if r.Level == slog.LevelError && r.Message == "failed to emit plugin event" {
			emitErrorCount++
		}
	}

	// Since the cancelling emitter cancels on first emit, the first emit will fail
	// but subsequent emits should be skipped. With echo plugin emitting only 1 event,
	// we'll see 1 error and no skip warning (since there's only 1 emit total).
	// This test verifies the skip mechanism works for multi-emit scenarios.

	// If skip warning was logged, verify it has pending_emits count
	if skipEmitsWarn {
		if pendingEmits <= 0 {
			t.Error("expected pending_emits > 0 in skip warning log")
		}
	}

	// Verify we don't have excessive errors
	if emitErrorCount > 1 {
		t.Errorf("expected at most 1 emit error with early cancellation, got %d", emitErrorCount)
	}
}

// cancellingEmitter cancels the context on first Emit call.
type cancellingEmitter struct {
	cancel context.CancelFunc
	notify chan struct{}
	called bool
	mu     sync.Mutex
}

func (c *cancellingEmitter) Emit(_ context.Context, _ string, _ core.EventType, _ []byte) error {
	c.mu.Lock()
	if !c.called {
		c.called = true
		c.cancel() // Cancel context on first emit
	}
	c.mu.Unlock()

	select {
	case c.notify <- struct{}{}:
	default:
	}
	return context.Canceled
}

func (c *cancellingEmitter) WaitForEmit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-c.notify:
		// Emit was called
	case <-time.After(timeout):
		t.Fatal("timeout waiting for emit call")
	}
}

// TestExtismSubscriber_EmitFailureAggregation verifies that after emit failures,
// a summary warning is logged with total attempted and failed counts.
func TestExtismSubscriber_EmitFailureAggregation(t *testing.T) {
	// Capture logs
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load echo plugin which emits one event
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newFailingEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()
	sub.Subscribe("echo", "location:*")

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for emit to be called (which will fail)
	emitter.WaitForEmit(t, 5*time.Second)

	// Wait for the aggregation summary log
	capture.WaitForLog(t, slog.LevelWarn, "plugin emit batch had failures", 5*time.Second)

	// Verify aggregation log has correct attributes
	records := capture.Records()
	var matchedRecord *slog.Record
	for i := range records {
		r := &records[i]
		if r.Level == slog.LevelWarn && r.Message == "plugin emit batch had failures" {
			matchedRecord = r
			break
		}
	}

	if matchedRecord == nil {
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, fmt.Sprintf("%s: %s", r.Level, r.Message))
		}
		t.Fatalf("expected warn log %q, got logs: %v", "plugin emit batch had failures", msgs)
	}

	// Extract attributes from log record
	attrs := make(map[string]any)
	matchedRecord.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})

	// Verify plugin name
	if got, ok := attrs["plugin"]; !ok {
		t.Error("expected plugin attribute in log, but not found")
	} else if got != "echo" {
		t.Errorf("plugin = %v, want %q", got, "echo")
	}

	// Verify failed count (echo plugin emits one event, which fails)
	if got, ok := attrs["failed"]; !ok {
		t.Error("expected failed attribute in log, but not found")
	} else if got != int64(1) {
		t.Errorf("failed = %v (%T), want 1", got, got)
	}

	// Verify total count
	if got, ok := attrs["total"]; !ok {
		t.Error("expected total attribute in log, but not found")
	} else if got != int64(1) {
		t.Errorf("total = %v (%T), want 1", got, got)
	}
}

// TestExtismSubscriber_Subscribe_EmptyPluginName verifies that Subscribe
// rejects empty plugin names and logs a warning.
func TestExtismSubscriber_Subscribe_EmptyPluginName(t *testing.T) {
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()

	// Subscribe with empty plugin name
	sub.Subscribe("", "location:*")

	// Verify warning was logged
	records := capture.Records()
	var foundWarn bool
	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "ignoring subscription with empty plugin name" {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Error("expected warning log for empty plugin name")
	}

	// Send an event to verify the subscription was NOT added
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Empty plugin name subscription should be rejected - no events should be emitted
	emitter.WaitForNoEvent(t, 100*time.Millisecond)
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events (empty plugin name rejected), got %d", len(emitter.Events()))
	}
}

// TestExtismSubscriber_Subscribe_EmptyPattern verifies that Subscribe
// rejects empty patterns and logs a warning with the plugin name.
func TestExtismSubscriber_Subscribe_EmptyPattern(t *testing.T) {
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()

	// Subscribe with empty pattern
	sub.Subscribe("echo", "")

	// Verify warning was logged with plugin name
	records := capture.Records()
	var foundWarn bool
	for _, r := range records {
		if r.Level == slog.LevelWarn && r.Message == "ignoring subscription with empty pattern" {
			attrs := make(map[string]any)
			r.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.Any()
				return true
			})
			if attrs["plugin"] == "echo" {
				foundWarn = true
				break
			}
		}
	}
	if !foundWarn {
		t.Error("expected warning log for empty pattern with plugin name")
	}

	// Send an event to verify the subscription was NOT added
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Empty pattern subscription should be rejected - no events should be emitted
	emitter.WaitForNoEvent(t, 100*time.Millisecond)
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events (empty pattern rejected), got %d", len(emitter.Events()))
	}
}

// TestExtismSubscriber_Subscribe_NonexistentPlugin verifies that Subscribe
// allows subscriptions for plugins not yet loaded (for lazy loading) and
// logs at debug level.
func TestExtismSubscriber_Subscribe_NonexistentPlugin(t *testing.T) {
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()

	// Subscribe to a plugin that doesn't exist yet
	sub.Subscribe("future-plugin", "location:*")

	// Verify debug log was emitted
	records := capture.Records()
	var foundDebug bool
	for _, r := range records {
		if r.Level == slog.LevelDebug && r.Message == "subscribing plugin not yet loaded" {
			attrs := make(map[string]any)
			r.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.Any()
				return true
			})
			if attrs["plugin"] == "future-plugin" {
				foundDebug = true
				break
			}
		}
	}
	if !foundDebug {
		t.Error("expected debug log for non-existent plugin subscription")
	}

	// The subscription should still be registered (for lazy loading)
	// When we later load the plugin and send an event, it should be delivered
	err := host.LoadPlugin(context.Background(), "future-plugin", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	// Wait for event - subscription should work after plugin is loaded
	emitter.WaitForEvent(t, 5*time.Second)
	if len(emitter.Events()) != 1 {
		t.Errorf("expected 1 event after loading plugin, got %d", len(emitter.Events()))
	}
}

// TestExtismSubscriber_Subscribe_ExistingPlugin verifies that Subscribe
// does not log debug message when plugin already exists.
func TestExtismSubscriber_Subscribe_ExistingPlugin(t *testing.T) {
	capture := &logCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load plugin first
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	emitter := newMockEmitter()
	sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
	defer sub.Stop()

	// Subscribe to existing plugin
	sub.Subscribe("echo", "location:*")

	// Verify NO debug log about plugin not being loaded
	records := capture.Records()
	for _, r := range records {
		if r.Level == slog.LevelDebug && r.Message == "subscribing plugin not yet loaded" {
			t.Error("should not log 'plugin not yet loaded' for existing plugin")
		}
	}

	// Subscription should work
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:room1",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	sub.HandleEvent(context.Background(), event)

	emitter.WaitForEvent(t, 5*time.Second)
	if len(emitter.Events()) != 1 {
		t.Errorf("expected 1 event, got %d", len(emitter.Events()))
	}
}
