package lua_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// writeMainLua creates a main.lua plugin file in the given directory.
func writeMainLua(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// closeHost closes the host and fails the test if an error occurs.
func closeHost(t *testing.T, host *pluginlua.Host) {
	t.Helper()
	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestLuaHost_Load(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{
			Entry: "main.lua",
		},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	plugins := host.Plugins()
	if len(plugins) != 1 || plugins[0] != "test-plugin" {
		t.Errorf("Plugins() = %v, want [test-plugin]", plugins)
	}
}

func TestLuaHost_DeliverEvent_ReturnsEmitEvents(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    if event.type == "say" then
        return {
            {
                stream = event.stream,
                type = "say",
                payload = '{"message":"Echo: ' .. event.payload .. '"}'
            }
        }
    end
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "echo",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{
		ID:        "01ABC",
		Stream:    "location:123",
		Type:      "say",
		Timestamp: 1705591234000,
		ActorKind: pluginpkg.ActorCharacter,
		ActorID:   "char_1",
		Payload:   "Hello",
	}

	emits, err := host.DeliverEvent(context.Background(), "echo", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 1 {
		t.Fatalf("len(emits) = %d, want 1", len(emits))
	}

	if emits[0].Stream != "location:123" {
		t.Errorf("emit.Stream = %q, want %q", emits[0].Stream, "location:123")
	}
}

func TestLuaHost_DeliverEvent_NoHandler(t *testing.T) {
	dir := t.TempDir()

	// Plugin without on_event function
	writeMainLua(t, dir, `x = 1`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "no-handler",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "no-handler", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 0 {
		t.Errorf("expected no emits for plugin without handler")
	}
}

func TestLuaHost_Unload(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	if len(host.Plugins()) != 1 {
		t.Fatalf("expected 1 plugin after load")
	}

	if err := host.Unload(context.Background(), "test-plugin"); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}

	if len(host.Plugins()) != 0 {
		t.Errorf("expected 0 plugins after unload, got %d", len(host.Plugins()))
	}
}

func TestLuaHost_Unload_NotFound(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	err := host.Unload(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when unloading nonexistent plugin")
	}
}

func TestLuaHost_DeliverEvent_NotLoaded(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "nonexistent", event)
	if err == nil {
		t.Error("expected error when delivering to nonexistent plugin")
	}
}

func TestLuaHost_Load_SyntaxError(t *testing.T) {
	dir := t.TempDir()

	// Invalid Lua syntax
	writeMainLua(t, dir, `function on_event(event return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "bad-syntax",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading plugin with syntax error")
	}
}

func TestLuaHost_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "missing-file",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "nonexistent.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading plugin with missing file")
	}
}

func TestLuaHost_Close(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()

	manifest := &plugin.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Should error when loading after close
	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading after close")
	}
}
