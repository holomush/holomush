// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventvocab"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// drainStream reads every stored message on EVENTS by walking sequences
// 1..LastSeq via GetMsg — a stateless RPC — so the helper does not spin up
// consumer goroutines that would race with t.Cleanup of the embedded bus.
func drainStream(t *testing.T, js jetstream.JetStream) []*jetstream.RawStreamMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := js.Stream(ctx, eventbus.StreamName)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	var out []*jetstream.RawStreamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && seq != 0; seq++ {
		msg, gerr := stream.GetMsg(ctx, seq)
		require.NoError(t, gerr)
		out = append(out, msg)
	}
	return out
}

// setupRoutingFixture creates a plugins directory with two Lua plugins:
//   - "say-plugin": a command plugin
//   - "echo-bot": an event plugin
func setupRoutingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua command plugin directory
	sayDir := filepath.Join(pluginsDir, "say-plugin")
	mkdirAll(t, sayDir)
	writeFile(t, filepath.Join(sayDir, "plugin.yaml"), []byte(`name: say-plugin
version: 1.0.0
type: lua
emits:
  - location
history_scope: grid
actor_kinds_claimable:
  - plugin
  - character
commands:
  - name: say
    help: Say something
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(sayDir, "main.lua"), []byte("function on_command(c) end"))

	// Create a Lua event plugin directory
	luaDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte(`name: echo-bot
version: 1.0.0
type: lua
emits:
  - location
history_scope: grid
actor_kinds_claimable:
  - plugin
  - character
events:
  - say
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte("function on_event(e) end"))

	return pluginsDir
}

type testEventEmitterHost struct {
	emitter     plugins.PluginIntentEmitter
	loadFn      func(context.Context, *plugins.Manifest, string) error
	loadedNames []string
}

func (h *testEventEmitterHost) SetEventEmitter(emitter plugins.PluginIntentEmitter) {
	h.emitter = emitter
}

func (h *testEventEmitterHost) Load(ctx context.Context, manifest *plugins.Manifest, dir string) error {
	h.loadedNames = append(h.loadedNames, manifest.Name)
	if h.loadFn != nil {
		return h.loadFn(ctx, manifest, dir)
	}
	return nil
}

func (h *testEventEmitterHost) Unload(context.Context, string) error { return nil }

func (h *testEventEmitterHost) DeliverEvent(context.Context, string, pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

func (h *testEventEmitterHost) DeliverCommand(context.Context, string, pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}

func (h *testEventEmitterHost) QuerySessionStreams(context.Context, string, plugins.SessionStreamsRequest) ([]string, error) {
	return nil, nil
}

func (h *testEventEmitterHost) Plugins() []string { return append([]string(nil), h.loadedNames...) }

func (h *testEventEmitterHost) PluginEmitRegistry(string) ([]string, bool) { return nil, false }

func (h *testEventEmitterHost) Close(context.Context) error { return nil }

func TestManagerRegisterHost(t *testing.T) {
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	mockHost := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeBinary, mockHost)

	// Registering another host for the same type replaces it
	mockHost2 := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeBinary, mockHost2)

	// No panic, no error -- just replacement
}

func TestManagerRegisterHostPanicsOnNil(t *testing.T) {
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	assert.Panics(t, func() {
		mgr.RegisterHost(plugins.TypeBinary, nil)
	})
}

func TestManagerRegisterHostBackfillsConfiguredEventEmitter(t *testing.T) {
	bootstrapReg, bootErr := core.BootstrapVerbRegistry("test")
	require.NoError(t, bootErr)
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(bootstrapReg))
	require.NoError(t, mgrErr)
	bus := eventbustest.New(t)

	mgr.ConfigureEventEmitter(bus.Bus.Publisher())

	host := &testEventEmitterHost{}
	mgr.RegisterHost(plugins.TypeBinary, host)

	require.NotNil(t, host.emitter)
}

func TestManagerLoadAllExposesInflightManifestToInitTimeEmitter(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	binDir := filepath.Join(pluginsDir, "scene-binary")
	mkdirAll(t, binDir)
	writeFile(t, filepath.Join(binDir, "plugin.yaml"), []byte(`name: scene-binary
version: 1.0.0
type: binary
emits:
  - scene
history_scope: scene
binary-plugin:
  executable: scene-binary
`))

	bus := eventbustest.New(t)
	host := &testEventEmitterHost{}
	host.loadFn = func(ctx context.Context, manifest *plugins.Manifest, _ string) error {
		if host.emitter == nil {
			return errors.New("event emitter was not injected")
		}
		// Post-w9ml: Actor.ID MUST be a ULID; use a deterministic fixture.
		emitCtx := core.WithActor(ctx, core.Actor{Kind: core.ActorPlugin, ID: fixturePluginULID.String()})
		return host.emitter.Emit(emitCtx, manifest.Name, pluginsdk.EmitIntent{
			Subject: "scene.test",
			Type:    pluginsdk.EventType(eventvocab.EventTypeSystem),
			Payload: `{"phase":"init"}`,
		})
	}

	bootstrapReg, bootErr := core.BootstrapVerbRegistry("test")
	require.NoError(t, bootErr)
	require.NoError(t, bootstrapReg.RegisterWithSource(core.VerbRegistration{
		Type:          "say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "1.0.0"))
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(bootstrapReg))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)
	mgr.ConfigureEventEmitter(bus.Bus.Publisher())

	require.NoError(t, mgr.LoadAll(context.Background()))

	msgs := drainStream(t, bus.JS)
	require.Len(t, msgs, 1)
	// Dot-relative "scene.test" → events.main.scene.test (default game_id).
	assert.Equal(t, "events.main.scene.test", msgs[0].Subject)
	// Post-w9ml: every stamp site emits a real ULID, so App-Actor-ID is
	// always present for plugin actors (matches fixturePluginULID above).
	assert.Equal(t, "plugin", msgs[0].Header.Get(eventbus.HeaderActorKind))
	assert.Equal(t, fixturePluginULID.String(), msgs[0].Header.Get(eventbus.HeaderActorID))

	var env eventbusv1.Event
	require.NoError(t, proto.Unmarshal(msgs[0].Data, &env))
	assert.Equal(t, `{"phase":"init"}`, string(env.GetPayload()))
}

func TestManagerDeliverCommandRoutesToCorrectHost(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	// Both plugins load via Lua host
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedResp := &pluginsdk.CommandResponse{Output: "hello world"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(expectedResp, nil)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 2)

	resp, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{
		Command: "say",
		Args:    "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Output)
}

func TestManagerDeliverCommandUnknownPlugin(t *testing.T) {
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	_, err := mgr.DeliverCommand(context.Background(), "nonexistent", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerDeliverEventRoutesToCorrectHost(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedEmits := []pluginsdk.EmitEvent{{Stream: "loc:1", Type: "say", Payload: `{}`}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(expectedEmits, nil)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))

	emits, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{
		Stream: "loc:1",
		Type:   pluginsdk.EventType("say"),
	})
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "loc:1", emits[0].Stream)
}

func TestManagerDeliverEventUnknownPlugin(t *testing.T) {
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	_, err := mgr.DeliverEvent(context.Background(), "nonexistent", pluginsdk.Event{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerEmitPluginEventUsesConfiguredSharedEmitter(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)
	// Need a location emit-capable manifest; setup fixture already declares
	// `emits: [location]` on say-plugin.
	bus := eventbustest.New(t)
	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	bootstrapReg, bootErr := core.BootstrapVerbRegistry("test")
	require.NoError(t, bootErr)
	require.NoError(t, bootstrapReg.RegisterWithSource(core.VerbRegistration{
		Type:          "say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "1.0.0"))
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(bootstrapReg))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	mgr.ConfigureEventEmitter(bus.Bus.Publisher())

	// Use a valid ULID so the bridge preserves the id in the App-Actor-ID
	// header (non-ULID strings are intentionally dropped, matching spec).
	charID := core.NewULID()
	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   charID.String(),
	})

	err := mgr.EmitPluginEvent(ctx, "say-plugin", pluginsdk.EmitEvent{
		Stream:  "location.123",
		Type:    pluginsdk.EventType("say"),
		Payload: `{"text":"hello"}`,
	})
	require.NoError(t, err)

	msgs := drainStream(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.main.location.123", msgs[0].Subject)
	assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
	assert.Equal(t, charID.String(), msgs[0].Header.Get(eventbus.HeaderActorID))
}

func TestManagerDeliverCommandConcurrentSafety(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	const goroutines = 10

	resp := &pluginsdk.CommandResponse{Output: "ok"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(resp, nil).Times(goroutines)
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(nil, nil).Times(goroutines)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestManagerLoadAllSkipsPluginsWithoutHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a binary plugin but don't register a binary host
	binDir := filepath.Join(pluginsDir, "my-binary")
	mkdirAll(t, binDir)
	writeFile(t, filepath.Join(binDir, "plugin.yaml"), []byte(`name: my-binary
version: 1.0.0
type: binary
binary-plugin:
  executable: my-binary
`))

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	require.NoError(t, mgr.LoadAll(context.Background()))

	// Plugin should be skipped since no binary host is registered
	assert.Empty(t, mgr.ListPlugins())
}

func TestManagerPluginHostMappingTrackedCorrectly(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	// Commands route to the right plugin
	sayResp := &pluginsdk.CommandResponse{Output: "from say-plugin"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(sayResp, nil)

	luaEmits := []pluginsdk.EmitEvent{{Stream: "s", Type: "say"}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(luaEmits, nil)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))

	// DeliverCommand to say-plugin
	resp, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
	require.NoError(t, err)
	assert.Equal(t, "from say-plugin", resp.Output)

	// DeliverEvent to echo-bot
	emits, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{})
	require.NoError(t, err)
	assert.Len(t, emits, 1)
}

func TestManagerCloseClearsPluginHostMapping(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	sayDir := filepath.Join(pluginsDir, "say-plugin")
	mkdirAll(t, sayDir)
	writeFile(t, filepath.Join(sayDir, "plugin.yaml"), []byte(`name: say-plugin
version: 1.0.0
type: lua
commands:
  - name: say
    help: Say something
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(sayDir, "main.lua"), []byte(""))

	mockLua := mocks.NewMockHost(t)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 1)

	require.NoError(t, mgr.Close(context.Background()))

	// After close, routing should fail
	_, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerCloseClosesAllHosts(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	// Host should be closed
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.NoError(t, mgr.Close(context.Background()))

	// Mock expectations verify Close() was called
}

// Verify the PluginPolicyInstaller test still works with new routing
func TestManagerLoadAllWithPoliciesMultiHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	luaDir := filepath.Join(pluginsDir, "policy-plugin")
	mkdirAll(t, luaDir)

	policyYAML := `name: policy-plugin
version: 1.0.0
type: lua
policies:
  - name: test-policy
    dsl: "allow admin all"
lua-plugin:
  entry: main.lua
`
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte(policyYAML))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte(""))

	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockHost.EXPECT().Close(mock.Anything).Return(nil)

	policyInstalled := false
	installer := &testPolicyInstaller{
		installFn: func(_ context.Context, name string, _ []plugins.ManifestPolicy) error {
			assert.Equal(t, "policy-plugin", name)
			policyInstalled = true
			return nil
		},
		removeFn: func(context.Context, string) error { return nil },
	}

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(mockHost),
		plugins.WithPolicyInstaller(installer),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	assert.True(t, policyInstalled)
	assert.Len(t, mgr.ListPlugins(), 1)
}

// testPolicyInstaller implements PluginPolicyInstaller for tests.
type testPolicyInstaller struct {
	installFn func(context.Context, string, []plugins.ManifestPolicy) error
	removeFn  func(context.Context, string) error
}

func (p *testPolicyInstaller) InstallPluginPolicies(ctx context.Context, name string, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, name, policies)
}

func (p *testPolicyInstaller) InstallPluginPoliciesWithManifest(ctx context.Context, manifest *plugins.Manifest, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, manifest.Name, policies)
}

func (p *testPolicyInstaller) RemovePluginPolicies(ctx context.Context, name string) error {
	return p.removeFn(ctx, name)
}

func (p *testPolicyInstaller) ReplacePluginPolicies(ctx context.Context, name string, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, name, policies)
}

func (p *testPolicyInstaller) ReplacePluginPoliciesWithManifest(ctx context.Context, manifest *plugins.Manifest, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, manifest.Name, policies)
}
