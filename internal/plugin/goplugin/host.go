// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package goplugin provides a Host implementation for binary plugins
// using HashiCorp's go-plugin system over gRPC.
package goplugin

import (
	"context"
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
func NewHost(enforcer *capability.Enforcer) *Host {
	return &Host{
		enforcer:      enforcer,
		clientFactory: &DefaultClientFactory{},
		plugins:       make(map[string]*loadedPlugin),
	}
}

// NewHostWithFactory creates a host with a custom client factory (for testing).
func NewHostWithFactory(enforcer *capability.Enforcer, factory ClientFactory) *Host {
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
		return fmt.Errorf("host is closed")
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return fmt.Errorf("plugin %s already loaded", manifest.Name)
	}

	if manifest.BinaryPlugin == nil {
		return fmt.Errorf("plugin %s is not a binary plugin", manifest.Name)
	}

	execPath := filepath.Join(dir, manifest.BinaryPlugin.Executable)
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin executable not found: %s", execPath)
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

	p, ok := h.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not loaded", name)
	}

	if p.client != nil {
		p.client.Kill()
	}

	if err := h.enforcer.RemoveGrants(name); err != nil {
		slog.Warn("failed to remove capabilities during unload",
			"plugin", name,
			"error", err)
	}

	delete(h.plugins, name)
	return nil
}

// DeliverEvent sends an event to a plugin and returns response events.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, fmt.Errorf("host is closed")
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("plugin %s not loaded", name)
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
		if err := h.enforcer.RemoveGrants(name); err != nil {
			slog.Warn("failed to remove capabilities during close",
				"plugin", name,
				"error", err)
		}
	}

	h.closed = true
	h.plugins = nil
	return nil
}
