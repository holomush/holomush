//go:build integration

package plugin_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// TestEchoBot_Integration tests the echo-bot plugin end-to-end.
// This test verifies:
// 1. Plugin discovery works for echo-bot
// 2. Plugin loads successfully with LuaHost
// 3. Echo bot receives say events and emits echo responses
// 4. Echo bot ignores events from plugins (prevents loops)
// 5. Capability enforcement works (events.emit.location)
func TestEchoBot_Integration(t *testing.T) {
	fixture := setupEchoBotTest(t)
	defer fixture.Cleanup()

	ctx := context.Background()
	luaHost := fixture.LuaHost
	echoBotPlugin := fixture.Plugin

	// Verify manifest
	if echoBotPlugin.Manifest.Type != plugin.TypeLua {
		t.Errorf("Manifest.Type = %v, want %v", echoBotPlugin.Manifest.Type, plugin.TypeLua)
	}
	if echoBotPlugin.Manifest.Version != "1.0.0" {
		t.Errorf("Manifest.Version = %v, want 1.0.0", echoBotPlugin.Manifest.Version)
	}

	// Verify events subscription
	if !slices.Contains(echoBotPlugin.Manifest.Events, "say") {
		t.Error("echo-bot manifest should have 'say' in events")
	}

	// Verify capabilities
	if !slices.Contains(echoBotPlugin.Manifest.Capabilities, "events.emit.location") {
		t.Error("echo-bot manifest should have 'events.emit.location' capability")
	}

	// Test: echo bot responds to say events from characters
	t.Run("responds to say events from characters", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01ABC",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypeSay,
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorCharacter,
			ActorID:   "char_1",
			Payload:   `{"message":"Hello, world!"}`,
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		if len(emits) != 1 {
			t.Fatalf("len(emits) = %d, want 1", len(emits))
		}

		// Verify emitted event
		if emits[0].Stream != "location:123" {
			t.Errorf("emit.Stream = %q, want %q", emits[0].Stream, "location:123")
		}
		if emits[0].Type != pluginpkg.EventTypeSay {
			t.Errorf("emit.Type = %q, want %q", emits[0].Type, pluginpkg.EventTypeSay)
		}

		// Parse payload and verify echo message
		var payload map[string]string
		if err := json.Unmarshal([]byte(emits[0].Payload), &payload); err != nil {
			t.Fatalf("Failed to parse payload: %v", err)
		}

		if !strings.HasPrefix(payload["message"], "Echo:") {
			t.Errorf("payload.message = %q, want to start with 'Echo:'", payload["message"])
		}
		if !strings.Contains(payload["message"], "Hello, world!") {
			t.Errorf("payload.message = %q, want to contain 'Hello, world!'", payload["message"])
		}
	})

	// Test: echo bot ignores events from plugins (prevents loops)
	t.Run("ignores events from plugins", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01DEF",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypeSay,
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorPlugin, // From a plugin!
			ActorID:   "some-plugin",
			Payload:   `{"message":"Echo: something"}`,
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		// Should not emit anything for plugin-originated events
		if len(emits) != 0 {
			t.Errorf("len(emits) = %d, want 0 (should ignore plugin events)", len(emits))
		}
	})

	// Test: echo bot ignores non-say events
	t.Run("ignores non-say events", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01GHI",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypePose, // Not a say event
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorCharacter,
			ActorID:   "char_1",
			Payload:   `{"message":"waves hello"}`,
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		if len(emits) != 0 {
			t.Errorf("len(emits) = %d, want 0 (should ignore non-say events)", len(emits))
		}
	})

	// Test: echo bot ignores empty messages
	t.Run("ignores empty messages", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01JKL",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypeSay,
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorCharacter,
			ActorID:   "char_1",
			Payload:   `{"message":""}`, // Empty message
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		if len(emits) != 0 {
			t.Errorf("len(emits) = %d, want 0 (should ignore empty messages)", len(emits))
		}
	})

	// Test: echo bot handles payload without message key
	t.Run("handles payload without message key", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01MNO",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypeSay,
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorCharacter,
			ActorID:   "char_1",
			Payload:   `{"text":"wrong key"}`, // No "message" key
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		if len(emits) != 0 {
			t.Errorf("len(emits) = %d, want 0 (should ignore payload without message key)", len(emits))
		}
	})

	// Test: echo bot handles empty payload
	t.Run("handles empty payload", func(t *testing.T) {
		event := pluginpkg.Event{
			ID:        "01PQR",
			Stream:    "location:123",
			Type:      pluginpkg.EventTypeSay,
			Timestamp: time.Now().UnixMilli(),
			ActorKind: pluginpkg.ActorCharacter,
			ActorID:   "char_1",
			Payload:   "", // Empty payload
		}

		emits, err := luaHost.DeliverEvent(ctx, "echo-bot", event)
		if err != nil {
			t.Fatalf("DeliverEvent() error = %v", err)
		}

		if len(emits) != 0 {
			t.Errorf("len(emits) = %d, want 0 (should handle empty payload gracefully)", len(emits))
		}
	})
}

// TestEchoBot_Subscriber tests the full event flow with Subscriber.
func TestEchoBot_Subscriber(t *testing.T) {
	fixture := setupEchoBotTest(t)
	defer fixture.Cleanup()

	// Create mock emitter to capture emitted events
	emitter := &mockEmitter{}

	// Create subscriber
	subscriber := plugin.NewSubscriber(fixture.LuaHost, emitter)
	subscriber.Subscribe("echo-bot", "location:123", fixture.Plugin.Manifest.Events)

	// Start subscriber with event channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 10)
	subscriber.Start(ctx, events)

	// Send a say event
	events <- pluginpkg.Event{
		ID:        "01STU",
		Stream:    "location:123",
		Type:      pluginpkg.EventTypeSay,
		Timestamp: time.Now().UnixMilli(),
		ActorKind: pluginpkg.ActorCharacter,
		ActorID:   "char_1",
		Payload:   `{"message":"Test message"}`,
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Stop subscriber
	cancel()
	subscriber.Stop()

	// Verify emitted event
	emitted := emitter.getEmitted()
	if len(emitted) != 1 {
		t.Fatalf("len(emitted) = %d, want 1", len(emitted))
	}

	if emitted[0].Stream != "location:123" {
		t.Errorf("emit.Stream = %q, want %q", emitted[0].Stream, "location:123")
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(emitted[0].Payload), &payload); err != nil {
		t.Fatalf("Failed to parse payload: %v", err)
	}

	if !strings.Contains(payload["message"], "Echo:") {
		t.Errorf("payload.message = %q, want to contain 'Echo:'", payload["message"])
	}
}

// echoBotFixture contains all components needed for echo-bot integration tests.
type echoBotFixture struct {
	LuaHost  *pluginlua.Host
	Enforcer *capability.Enforcer
	Plugin   *plugin.DiscoveredPlugin
	Cleanup  func()
}

// setupEchoBotTest creates all components needed to test the echo-bot plugin.
func setupEchoBotTest(t *testing.T) *echoBotFixture {
	t.Helper()

	pluginsDir := findPluginsDir(t)
	echoBotDir := filepath.Join(pluginsDir, "echo-bot")

	if _, err := os.Stat(echoBotDir); os.IsNotExist(err) {
		t.Fatalf("echo-bot plugin not found at %s", echoBotDir)
	}

	enforcer := capability.NewEnforcer()
	hostFuncs := hostfunc.New(nil, enforcer)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	manager := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		t.Fatalf("Discover() error = %v", err)
	}

	var echoBotPlugin *plugin.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "echo-bot" {
			echoBotPlugin = dp
			break
		}
	}

	if echoBotPlugin == nil {
		luaHost.Close(ctx) //nolint:errcheck
		t.Fatal("echo-bot plugin not discovered")
	}

	if err := luaHost.Load(ctx, echoBotPlugin.Manifest, echoBotPlugin.Dir); err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		t.Fatalf("Load() error = %v", err)
	}

	if err := enforcer.SetGrants("echo-bot", echoBotPlugin.Manifest.Capabilities); err != nil {
		luaHost.Close(ctx) //nolint:errcheck
		t.Fatalf("SetGrants() error = %v", err)
	}

	return &echoBotFixture{
		LuaHost:  luaHost,
		Enforcer: enforcer,
		Plugin:   echoBotPlugin,
		Cleanup: func() {
			if err := luaHost.Close(context.Background()); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		},
	}
}

// findPluginsDir locates the plugins directory relative to the test.
func findPluginsDir(t *testing.T) string {
	t.Helper()

	// Try relative paths from test location
	candidates := []string{
		"../../plugins",       // From internal/plugin
		"../../../plugins",    // If test is deeper
		"./plugins",           // Current directory
		"../../../../plugins", // Deeper nesting
	}

	// Get current working directory to help debug
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	for _, candidate := range candidates {
		path := filepath.Join(cwd, candidate)
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	t.Fatalf("Could not find plugins directory from %s", cwd)
	return ""
}

// mockEmitter captures emitted events for testing with thread-safe access.
type mockEmitter struct {
	mu      sync.Mutex
	emitted []pluginpkg.EmitEvent
}

func (e *mockEmitter) EmitPluginEvent(_ context.Context, _ string, event pluginpkg.EmitEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, event)
	return nil
}

func (e *mockEmitter) getEmitted() []pluginpkg.EmitEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]pluginpkg.EmitEvent, len(e.emitted))
	copy(result, e.emitted)
	return result
}
