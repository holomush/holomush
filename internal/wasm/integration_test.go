//go:build integration

package wasm_test

import (
	"context"
	_ "embed"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/wasm"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/trace/noop"
)

//go:embed testdata/echo.wasm
var echoWASMIntegration []byte

// collectingEmitter collects emitted events for verification.
type collectingEmitter struct {
	mu     sync.Mutex
	events []core.Event
}

func (e *collectingEmitter) Emit(_ context.Context, stream string, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, core.Event{
		Stream:  stream,
		Type:    eventType,
		Payload: payload,
	})
	return nil
}

func (e *collectingEmitter) Events() []core.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]core.Event{}, e.events...)
}

// TestExtism_Integration verifies the complete Extism plugin flow:
// load plugin → subscribe → deliver event → verify emitted response.
func TestExtism_Integration(t *testing.T) {
	ctx := context.Background()
	tracer := noop.NewTracerProvider().Tracer("integration-test")

	// Step 1: Create ExtismHost
	host := wasm.NewExtismHost(tracer)
	defer func() {
		if err := host.Close(ctx); err != nil {
			t.Errorf("failed to close host: %v", err)
		}
	}()

	// Step 2: Load the echo plugin
	err := host.LoadPlugin(ctx, "echo", echoWASMIntegration)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Verify plugin is loaded
	if !host.HasPlugin("echo") {
		t.Fatal("expected echo plugin to be loaded")
	}

	// Step 3: Create subscriber and register subscription
	emitter := &collectingEmitter{}
	subscriber := wasm.NewExtismSubscriber(host, emitter)
	subscriber.Subscribe("echo", "location:*")

	// Step 4: Deliver a say event
	testMessage := "Hello, integration test!"
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test-room",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test-player"},
		Payload:   []byte(`{"message":"` + testMessage + `"}`),
	}

	subscriber.HandleEvent(ctx, event)

	// Step 5: Wait for async processing
	time.Sleep(3 * time.Second)

	// Step 6: Verify emitted response
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(events))
	}

	emittedEvent := events[0]

	// Verify event type is say
	if emittedEvent.Type != core.EventTypeSay {
		t.Errorf("expected event type %q, got %q", core.EventTypeSay, emittedEvent.Type)
	}

	// Verify stream matches
	if emittedEvent.Stream != "location:test-room" {
		t.Errorf("expected stream %q, got %q", "location:test-room", emittedEvent.Stream)
	}

	// Verify payload contains "Echo:" prefix
	payloadStr := string(emittedEvent.Payload)
	if !strings.Contains(payloadStr, "Echo:") {
		t.Errorf("expected payload to contain 'Echo:', got %q", payloadStr)
	}

	// Verify original message is echoed
	if !strings.Contains(payloadStr, testMessage) {
		t.Errorf("expected payload to contain original message %q, got %q", testMessage, payloadStr)
	}

	t.Logf("Integration test passed: received echo response with payload: %s", payloadStr)
}
