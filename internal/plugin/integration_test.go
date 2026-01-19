package plugin_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	// Find the echo-bot plugin in the plugins directory
	// The plugin should be at plugins/echo-bot relative to repo root
	pluginsDir := findPluginsDir(t)
	echoBotDir := filepath.Join(pluginsDir, "echo-bot")

	// Verify plugin directory exists
	if _, err := os.Stat(echoBotDir); os.IsNotExist(err) {
		t.Fatalf("echo-bot plugin not found at %s", echoBotDir)
	}

	// Create manager and discover plugins
	enforcer := capability.NewEnforcer()
	hostFuncs := hostfunc.New(nil, enforcer)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)
	defer func() {
		if err := luaHost.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	manager := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))

	// Discover plugins
	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// Find echo-bot in discovered plugins
	var echoBotPlugin *plugin.DiscoveredPlugin
	for _, dp := range discovered {
		if dp.Manifest.Name == "echo-bot" {
			echoBotPlugin = dp
			break
		}
	}

	if echoBotPlugin == nil {
		t.Fatal("echo-bot plugin not discovered")
	}

	// Verify manifest
	if echoBotPlugin.Manifest.Type != plugin.TypeLua {
		t.Errorf("Manifest.Type = %v, want %v", echoBotPlugin.Manifest.Type, plugin.TypeLua)
	}
	if echoBotPlugin.Manifest.Version != "1.0.0" {
		t.Errorf("Manifest.Version = %v, want 1.0.0", echoBotPlugin.Manifest.Version)
	}

	// Verify events subscription
	hasEvents := false
	for _, evt := range echoBotPlugin.Manifest.Events {
		if evt == "say" {
			hasEvents = true
			break
		}
	}
	if !hasEvents {
		t.Error("echo-bot manifest should have 'say' in events")
	}

	// Verify capabilities
	hasCapability := false
	for _, cap := range echoBotPlugin.Manifest.Capabilities {
		if cap == "events.emit.location" {
			hasCapability = true
			break
		}
	}
	if !hasCapability {
		t.Error("echo-bot manifest should have 'events.emit.location' capability")
	}

	// Load the plugin
	if err := luaHost.Load(ctx, echoBotPlugin.Manifest, echoBotPlugin.Dir); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Grant capabilities to the plugin
	if err := enforcer.SetGrants("echo-bot", echoBotPlugin.Manifest.Capabilities); err != nil {
		t.Fatalf("SetGrants() error = %v", err)
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
}

// TestEchoBot_Subscriber tests the full event flow with Subscriber.
func TestEchoBot_Subscriber(t *testing.T) {
	pluginsDir := findPluginsDir(t)
	echoBotDir := filepath.Join(pluginsDir, "echo-bot")

	if _, err := os.Stat(echoBotDir); os.IsNotExist(err) {
		t.Fatalf("echo-bot plugin not found at %s", echoBotDir)
	}

	// Set up components
	enforcer := capability.NewEnforcer()
	hostFuncs := hostfunc.New(nil, enforcer)
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)
	defer func() {
		if err := luaHost.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	manager := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))

	ctx := context.Background()
	discovered, err := manager.Discover(ctx)
	if err != nil {
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
		t.Fatal("echo-bot plugin not discovered")
	}

	// Load and configure
	if err := luaHost.Load(ctx, echoBotPlugin.Manifest, echoBotPlugin.Dir); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := enforcer.SetGrants("echo-bot", echoBotPlugin.Manifest.Capabilities); err != nil {
		t.Fatalf("SetGrants() error = %v", err)
	}

	// Create mock emitter to capture emitted events
	emitter := &mockEmitter{}

	// Create subscriber
	subscriber := plugin.NewSubscriber(luaHost, emitter)
	subscriber.Subscribe("echo-bot", "location:123", echoBotPlugin.Manifest.Events)

	// Start subscriber with event channel
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan pluginpkg.Event, 10)
	subscriber.Start(ctx, events)

	// Send a say event
	events <- pluginpkg.Event{
		ID:        "01MNO",
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

// mockEmitter captures emitted events for testing.
type mockEmitter struct {
	emitted []pluginpkg.EmitEvent
}

func (e *mockEmitter) EmitPluginEvent(_ context.Context, _ string, event pluginpkg.EmitEvent) error {
	e.emitted = append(e.emitted, event)
	return nil
}

func (e *mockEmitter) getEmitted() []pluginpkg.EmitEvent {
	return e.emitted
}
