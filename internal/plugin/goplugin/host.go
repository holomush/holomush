// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package goplugin provides a Host implementation for binary plugins
// using HashiCorp's go-plugin system over gRPC.
package goplugin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// DefaultEventTimeout is the default timeout for plugin event handling.
const DefaultEventTimeout = 5 * time.Second

// Sentinel errors for programmatic error checking.
var (
	// ErrHostClosed is returned when operations are attempted on a closed host.
	ErrHostClosed = errors.New("host is closed")
	// ErrPluginNotLoaded is returned when operating on a plugin that isn't loaded.
	ErrPluginNotLoaded = errors.New("plugin not loaded")
	// ErrPluginAlreadyLoaded is returned when loading a plugin that's already loaded.
	ErrPluginAlreadyLoaded = errors.New("plugin already loaded")
)

// Compile-time interface check.
var _ plugin.Host = (*Host)(nil)

// PluginClient wraps go-plugin client for testability.
type PluginClient interface {
	// Client returns the gRPC client protocol.
	Client() (hashiplug.ClientProtocol, error)
	// Kill terminates the plugin process.
	Kill()
}

// ClientFactory creates plugin clients.
type ClientFactory interface {
	// NewClient creates a client for the given executable path.
	NewClient(execPath string) PluginClient
}

// DefaultClientFactory creates real go-plugin clients.
type DefaultClientFactory struct{}

// NewClient creates a real go-plugin client.
func (f *DefaultClientFactory) NewClient(execPath string) PluginClient {
	return hashiplug.NewClient(&hashiplug.ClientConfig{
		HandshakeConfig:  HandshakeConfig,
		Plugins:          PluginMap,
		Cmd:              exec.Command(execPath), // #nosec G204 -- execPath resolved from plugin manifest; manifests validated during discovery
		AllowedProtocols: []hashiplug.Protocol{hashiplug.ProtocolGRPC},
	})
}

// Host manages binary plugins via HashiCorp go-plugin.
type Host struct {
	enforcer      *capability.Enforcer
	clientFactory ClientFactory
	plugins       map[string]*loadedPlugin
	mu            sync.RWMutex
	closed        bool
}

// loadedPlugin holds state for a single loaded binary plugin.
type loadedPlugin struct {
	manifest *plugin.Manifest
	client   PluginClient
	plugin   pluginv1.PluginClient
}

// NewHost creates a new binary plugin host.
// Panics if enforcer is nil.
func NewHost(enforcer *capability.Enforcer) *Host {
	return NewHostWithFactory(enforcer, &DefaultClientFactory{})
}

// NewHostWithFactory creates a host with a custom client factory (for testing).
// Panics if enforcer or factory is nil.
func NewHostWithFactory(enforcer *capability.Enforcer, factory ClientFactory) *Host {
	if enforcer == nil {
		panic("goplugin: enforcer cannot be nil")
	}
	if factory == nil {
		panic("goplugin: factory cannot be nil")
	}
	return &Host{
		enforcer:      enforcer,
		clientFactory: factory,
		plugins:       make(map[string]*loadedPlugin),
	}
}

// Load initializes a plugin from its manifest.
func (h *Host) Load(ctx context.Context, manifest *plugin.Manifest, dir string) error {
	// Check context before expensive operations
	if err := ctx.Err(); err != nil {
		return oops.In("goplugin").With("operation", "load").Wrap(err)
	}

	if manifest == nil {
		return oops.In("goplugin").With("operation", "load").New("manifest cannot be nil")
	}

	if manifest.Name == "" {
		return oops.In("goplugin").With("operation", "load").New("plugin name cannot be empty")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").Wrap(ErrPluginAlreadyLoaded)
	}

	if manifest.BinaryPlugin == nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").New("not a binary plugin")
	}

	execPath := filepath.Join(dir, manifest.BinaryPlugin.Executable)

	// Verify resolved path is within the plugin directory (prevent path traversal)
	// Use EvalSymlinks to resolve symlinks and prevent symlink-based escapes
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("dir", dir).Hint("cannot resolve plugin directory").Wrap(err)
	}
	realExec, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		if os.IsNotExist(err) {
			return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", execPath).Hint("plugin executable not found").Wrap(err)
		}
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", execPath).Hint("cannot resolve executable path").Wrap(err)
	}
	// Use filepath.Rel for robust cross-platform path containment check
	rel, err := filepath.Rel(realDir, realExec)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("executable", manifest.BinaryPlugin.Executable).New("plugin executable path escapes plugin directory")
	}

	// Use realExec (resolved symlink) for stat and client to prevent TOCTOU attacks
	info, err := os.Stat(realExec)
	if err != nil {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", realExec).Hint("cannot access plugin executable").Wrap(err)
	}
	// Check execute permission (user, group, or other)
	if info.Mode()&0o111 == 0 {
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").With("path", realExec).New("plugin executable not executable")
	}

	client := h.clientFactory.NewClient(realExec)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "connect").Wrap(err)
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "dispense").Wrap(err)
	}

	pluginClient, ok := raw.(pluginv1.PluginClient)
	if !ok {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "load").New("plugin does not implement PluginClient")
	}

	if err := h.enforcer.SetGrants(manifest.Name, manifest.Capabilities); err != nil {
		client.Kill()
		return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "set_capabilities").Wrap(err)
	}

	h.plugins[manifest.Name] = &loadedPlugin{
		manifest: manifest,
		client:   client,
		plugin:   pluginClient,
	}

	return nil
}

// Unload tears down a plugin.
func (h *Host) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	p, ok := h.plugins[name]
	if !ok {
		return oops.In("goplugin").With("plugin", name).With("operation", "unload").Wrap(ErrPluginNotLoaded)
	}

	if p.client != nil {
		p.client.Kill()
	}

	// RemoveGrants only fails if plugin name is empty, which cannot happen here
	// since we retrieved 'name' from h.plugins map (validated at Load time).
	// Warn-and-continue is safe; grants are cleaned up, unload proceeds.
	if err := h.enforcer.RemoveGrants(name); err != nil {
		slog.Warn("failed to remove capabilities during unload",
			"plugin", name,
			"error", err)
	}

	delete(h.plugins, name)
	return nil
}

// DeliverEvent sends an event to a plugin and returns response events.
//
// Note: The RLock is released before making the gRPC call to avoid serializing
// all plugin calls. If Close() or Unload() is called concurrently, the gRPC
// call will fail gracefully when the plugin process is killed. This is the
// standard trade-off in go-plugin based systems.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrHostClosed
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "deliver_event").Wrap(ErrPluginNotLoaded)
	}

	// Log warning for unrecognized actor kinds (useful for debugging)
	actorKind := event.ActorKind.String()
	if actorKind == "unknown" {
		slog.Warn("unrecognized actor kind, using 'unknown'",
			"kind", int(event.ActorKind))
	}

	protoEvent := &pluginv1.Event{
		Id:        event.ID,
		Stream:    event.Stream,
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		ActorKind: actorKind,
		ActorId:   event.ActorID,
		Payload:   event.Payload,
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	resp, err := p.plugin.HandleEvent(callCtx, &pluginv1.HandleEventRequest{Event: protoEvent})
	if err != nil {
		return nil, oops.In("goplugin").With("plugin", name).With("operation", "handle_event").Wrap(err)
	}

	emits := make([]pluginpkg.EmitEvent, len(resp.GetEmitEvents()))
	for i, e := range resp.GetEmitEvents() {
		emits[i] = pluginpkg.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginpkg.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}

	return emits, nil
}

// Plugins returns names of all loaded plugins.
func (h *Host) Plugins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return nil
	}

	names := make([]string, 0, len(h.plugins))
	for name := range h.plugins {
		names = append(names, name)
	}
	return names
}

// Close shuts down the host and all plugins.
func (h *Host) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	for name, p := range h.plugins {
		if p.client != nil {
			p.client.Kill()
		}
		// RemoveGrants only fails if plugin name is empty, which cannot happen
		// here since 'name' comes from h.plugins map keys (validated at Load).
		if err := h.enforcer.RemoveGrants(name); err != nil {
			slog.Warn("failed to remove capabilities during close",
				"plugin", name,
				"error", err)
		}
	}

	h.closed = true
	clear(h.plugins)
	return nil
}
