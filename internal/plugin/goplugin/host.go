// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package goplugin provides a Host implementation for binary plugins
// using HashiCorp's go-plugin system over gRPC.
package goplugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
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
	if enforcer == nil {
		panic("goplugin: enforcer cannot be nil")
	}
	return &Host{
		enforcer:      enforcer,
		clientFactory: &DefaultClientFactory{},
		plugins:       make(map[string]*loadedPlugin),
	}
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
func (h *Host) Load(_ context.Context, manifest *plugin.Manifest, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return fmt.Errorf("%w: %s", ErrPluginAlreadyLoaded, manifest.Name)
	}

	if manifest.BinaryPlugin == nil {
		return fmt.Errorf("plugin %s is not a binary plugin", manifest.Name)
	}

	execPath := filepath.Join(dir, manifest.BinaryPlugin.Executable)
	if _, err := os.Stat(execPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin executable not found: %s: %w", execPath, err)
		}
		return fmt.Errorf("cannot access plugin executable %s: %w", execPath, err)
	}

	client := h.clientFactory.NewClient(execPath)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to connect to plugin %s: %w", manifest.Name, err)
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to dispense plugin %s: %w", manifest.Name, err)
	}

	pluginClient, ok := raw.(pluginv1.PluginClient)
	if !ok {
		client.Kill()
		return fmt.Errorf("plugin %s does not implement PluginClient", manifest.Name)
	}

	if err := h.enforcer.SetGrants(manifest.Name, manifest.Capabilities); err != nil {
		client.Kill()
		return fmt.Errorf("failed to set capabilities for plugin %s: %w", manifest.Name, err)
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
		return fmt.Errorf("%w: %s", ErrPluginNotLoaded, name)
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
		return nil, fmt.Errorf("%w: %s", ErrPluginNotLoaded, name)
	}

	protoEvent := &pluginv1.Event{
		Id:        event.ID,
		Stream:    event.Stream,
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		ActorKind: actorKindToString(event.ActorKind),
		ActorId:   event.ActorID,
		Payload:   event.Payload,
	}

	callCtx, cancel := context.WithTimeout(ctx, DefaultEventTimeout)
	defer cancel()

	resp, err := p.plugin.HandleEvent(callCtx, &pluginv1.HandleEventRequest{Event: protoEvent})
	if err != nil {
		return nil, fmt.Errorf("plugin %s HandleEvent failed: %w", name, err)
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

// actorKindToString converts ActorKind to string.
func actorKindToString(kind pluginpkg.ActorKind) string {
	switch kind {
	case pluginpkg.ActorCharacter:
		return "character"
	case pluginpkg.ActorSystem:
		return "system"
	case pluginpkg.ActorPlugin:
		return "plugin"
	default:
		slog.Warn("unrecognized actor kind, using 'unknown'",
			"kind", int(kind))
		return "unknown"
	}
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
