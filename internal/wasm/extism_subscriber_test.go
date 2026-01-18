package wasm_test

import (
	"context"
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
}

func (m *mockEmitter) Emit(_ context.Context, stream string, eventType core.EventType, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitted = append(m.emitted, core.Event{
		Stream:  stream,
		Type:    eventType,
		Payload: payload,
	})
	return nil
}

func (m *mockEmitter) Events() []core.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]core.Event{}, m.emitted...)
}

func TestExtismSubscriber_Subscribe(t *testing.T) {
	t.Parallel()

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	emitter := &mockEmitter{}
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

	emitter := &mockEmitter{}
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
	time.Sleep(50 * time.Millisecond)

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

	emitter := &mockEmitter{}
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

	// Wait for async processing
	time.Sleep(2 * time.Second)

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

	emitter := &mockEmitter{}
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
	time.Sleep(100 * time.Millisecond)

	// No events emitted since plugin doesn't exist
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events, got %d", len(emitter.Events()))
	}
}

// failingEmitter always returns an error on Emit.
type failingEmitter struct{}

func (f *failingEmitter) Emit(_ context.Context, _ string, _ core.EventType, _ []byte) error {
	return context.DeadlineExceeded // Simulate failure
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

	emitter := &failingEmitter{}
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
	time.Sleep(2 * time.Second)
	// Test passes if no panic occurred - emitter failure is logged, not fatal
}

// slowEmitter blocks on Emit for a configurable duration.
type slowEmitter struct {
	mu      sync.Mutex
	delay   time.Duration
	emitted []core.Event
	started chan struct{}
}

func newSlowEmitter(delay time.Duration) *slowEmitter {
	return &slowEmitter{
		delay:   delay,
		started: make(chan struct{}, 10),
	}
}

func (s *slowEmitter) Emit(_ context.Context, stream string, eventType core.EventType, payload []byte) error {
	s.started <- struct{}{}
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitted = append(s.emitted, core.Event{
		Stream:  stream,
		Type:    eventType,
		Payload: payload,
	})
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

	// Create emitter that delays for 500ms
	emitter := newSlowEmitter(500 * time.Millisecond)
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

	emitter := &mockEmitter{}
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

	// Wait briefly and verify no events were emitted
	time.Sleep(200 * time.Millisecond)
	if len(emitter.Events()) != 0 {
		t.Errorf("expected 0 events after Stop, got %d", len(emitter.Events()))
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

// TestExtismSubscriber_ErrorLogging verifies that errors are logged via slog.Error.
// This tests the error handling strategy documented in deliverWithTimeout.
func TestExtismSubscriber_ErrorLogging(t *testing.T) {
	tests := []struct {
		name            string
		setupPlugin     bool                // whether to load a plugin
		setupEmitter    func() wasm.Emitter // emitter factory
		expectedMessage string              // expected log message substring
		expectEventInfo bool                // whether to verify event_id, event_stream, event_timestamp
	}{
		{
			name:            "plugin not found logs error with event info",
			setupPlugin:     false, // deliberately don't load plugin
			setupEmitter:    func() wasm.Emitter { return &mockEmitter{} },
			expectedMessage: "plugin event delivery failed",
			expectEventInfo: true, // delivery failure should include event info
		},
		{
			name:        "emitter failure logs error",
			setupPlugin: true,
			setupEmitter: func() wasm.Emitter {
				return &failingEmitter{}
			},
			expectedMessage: "failed to emit plugin event",
			expectEventInfo: false, // emit failure doesn't include event info
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture logs
			capture := &logCapture{}
			oldLogger := slog.Default()
			slog.SetDefault(slog.New(capture))
			defer slog.SetDefault(oldLogger)

			tracer := noop.NewTracerProvider().Tracer("test")
			host := wasm.NewExtismHost(tracer)
			defer func() { _ = host.Close(context.Background()) }()

			if tt.setupPlugin {
				err := host.LoadPlugin(context.Background(), "echo", echoWASM)
				if err != nil {
					t.Fatalf("LoadPlugin failed: %v", err)
				}
			}

			emitter := tt.setupEmitter()
			sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
			defer sub.Stop()
			sub.Subscribe("echo", "location:*")

			eventID := ulid.Make()
			eventStream := "location:room1"
			eventTimestamp := time.Now().Truncate(time.Second) // Truncate for comparison

			event := core.Event{
				ID:        eventID,
				Stream:    eventStream,
				Type:      core.EventTypeSay,
				Timestamp: eventTimestamp,
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
				Payload:   []byte(`{"message":"test"}`),
			}

			sub.HandleEvent(context.Background(), event)
			time.Sleep(2 * time.Second) // Wait for async processing

			// Verify error was logged
			records := capture.Records()
			var matchedRecord *slog.Record
			for i := range records {
				r := &records[i]
				if r.Level == slog.LevelError && r.Message == tt.expectedMessage {
					matchedRecord = r
					break
				}
			}

			if matchedRecord == nil {
				msgs := make([]string, 0, len(records))
				for _, r := range records {
					msgs = append(msgs, r.Message)
				}
				t.Fatalf("expected error log %q, got logs: %v", tt.expectedMessage, msgs)
			}

			// Verify event info attributes if expected
			if tt.expectEventInfo {
				attrs := make(map[string]any)
				matchedRecord.Attrs(func(a slog.Attr) bool {
					attrs[a.Key] = a.Value.Any()
					return true
				})

				// Check event_id
				if gotID, ok := attrs["event_id"]; !ok {
					t.Error("expected event_id attribute in log, but not found")
				} else if gotID != eventID.String() {
					t.Errorf("event_id = %v, want %v", gotID, eventID.String())
				}

				// Check event_stream
				if gotStream, ok := attrs["event_stream"]; !ok {
					t.Error("expected event_stream attribute in log, but not found")
				} else if gotStream != eventStream {
					t.Errorf("event_stream = %v, want %v", gotStream, eventStream)
				}

				// Check event_timestamp
				if _, ok := attrs["event_timestamp"]; !ok {
					t.Error("expected event_timestamp attribute in log, but not found")
				}
			}
		})
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
		description string
	}{
		// Exact match cases
		{
			name:        "exact match succeeds",
			pattern:     "location:room1",
			eventStream: "location:room1",
			expectMatch: true,
			description: "identical strings should match",
		},
		{
			name:        "exact mismatch by suffix",
			pattern:     "location:room1",
			eventStream: "location:room123",
			expectMatch: false,
			description: "stream with extra characters should not match exact pattern",
		},
		{
			name:        "exact mismatch by prefix",
			pattern:     "location:room1",
			eventStream: "location:room",
			expectMatch: false,
			description: "stream missing characters should not match exact pattern",
		},
		{
			name:        "exact mismatch completely different",
			pattern:     "location:room1",
			eventStream: "global:chat",
			expectMatch: false,
			description: "completely different streams should not match",
		},
		// Glob pattern cases
		{
			name:        "glob matches any suffix",
			pattern:     "location:*",
			eventStream: "location:room1",
			expectMatch: true,
			description: "glob pattern should match any suffix after prefix",
		},
		{
			name:        "glob matches empty suffix",
			pattern:     "location:*",
			eventStream: "location:",
			expectMatch: true,
			description: "glob pattern should match empty suffix",
		},
		{
			name:        "glob does not match different prefix",
			pattern:     "location:*",
			eventStream: "global:chat",
			expectMatch: false,
			description: "glob pattern should not match different prefix",
		},
		// Edge cases
		{
			name:        "single star matches everything",
			pattern:     "*",
			eventStream: "anything:goes:here",
			expectMatch: true,
			description: "single * should match any stream",
		},
		{
			name:        "empty pattern matches empty stream",
			pattern:     "",
			eventStream: "",
			expectMatch: true,
			description: "empty pattern should match empty stream exactly",
		},
		{
			name:        "empty pattern does not match non-empty stream",
			pattern:     "",
			eventStream: "location:room1",
			expectMatch: false,
			description: "empty pattern should not match non-empty stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracer := noop.NewTracerProvider().Tracer("test")
			host := wasm.NewExtismHost(tracer)
			defer func() { _ = host.Close(context.Background()) }()

			// Load the echo plugin which emits events when it receives them
			err := host.LoadPlugin(context.Background(), "echo", echoWASM)
			if err != nil {
				t.Fatalf("LoadPlugin failed: %v", err)
			}

			emitter := &mockEmitter{}
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

			// Wait for async processing
			time.Sleep(2 * time.Second)

			events := emitter.Events()
			gotMatch := len(events) > 0

			if gotMatch != tt.expectMatch {
				t.Errorf("%s: pattern=%q stream=%q: got match=%v, want match=%v",
					tt.description, tt.pattern, tt.eventStream, gotMatch, tt.expectMatch)
			}
		})
	}
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

	emitter := &failingEmitter{}
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
	time.Sleep(2 * time.Second) // Wait for async processing

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
