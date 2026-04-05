// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/oops"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Sentinel errors for LocalPluginHost.
var (
	// ErrHostClosed is returned when operations are attempted on a closed host.
	ErrHostClosed = errors.New("host is closed")
	// ErrPluginNotLoaded is returned when operating on a plugin that isn't loaded.
	ErrPluginNotLoaded = errors.New("plugin not loaded")
	// ErrNoCommandHandler is returned when DeliverCommand is called on a plugin without a command handler.
	ErrNoCommandHandler = errors.New("plugin has no command handler")
	// ErrNoEventHandler is returned when DeliverEvent is called on a plugin without an event handler.
	ErrNoEventHandler = errors.New("plugin has no event handler")
)

// LocalCommandHandler is implemented by in-process Go plugin command handlers.
type LocalCommandHandler interface {
	HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy ServiceProxy) (*pluginsdk.CommandResponse, error)
}

// LocalEventHandler is implemented by in-process Go plugin event handlers.
type LocalEventHandler interface {
	HandleEvent(ctx context.Context, event pluginsdk.Event, proxy ServiceProxy) ([]pluginsdk.EmitEvent, error)
}

// Compile-time interface check.
var _ Host = (*LocalPluginHost)(nil)

// localPlugin holds state for a single loaded in-process plugin.
type localPlugin struct {
	manifest       *Manifest
	commandHandler LocalCommandHandler // may be nil
	eventHandler   LocalEventHandler   // may be nil
}

// LocalPluginHost manages in-process Go plugins that implement the Host interface.
// It serves as a general-purpose in-process host for plugins whose handlers are
// wired programmatically rather than loaded from an external runtime.
type LocalPluginHost struct {
	mu      sync.RWMutex
	plugins map[string]*localPlugin
	proxy   ServiceProxy
	closed  bool
}

// NewLocalPluginHost creates a new LocalPluginHost with the given service proxy.
func NewLocalPluginHost(proxy ServiceProxy) *LocalPluginHost {
	return &LocalPluginHost{
		plugins: make(map[string]*localPlugin),
		proxy:   proxy,
	}
}

// Load registers the plugin manifest. The plugin is loaded with nil handlers;
// use the returned host to wire handlers via direct method calls if needed.
func (h *LocalPluginHost) Load(_ context.Context, manifest *Manifest, _ string) error {
	if manifest == nil {
		return oops.In("local").With("operation", "load").New("manifest cannot be nil")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("local").With("plugin", manifest.Name).With("operation", "load").Wrap(ErrHostClosed)
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return oops.In("local").With("plugin", manifest.Name).With("operation", "load").New("plugin already loaded")
	}

	h.plugins[manifest.Name] = &localPlugin{
		manifest: manifest,
	}

	return nil
}

// Unload removes a plugin.
func (h *LocalPluginHost) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("local").With("plugin", name).With("operation", "unload").Wrap(ErrHostClosed)
	}

	if _, ok := h.plugins[name]; !ok {
		return oops.In("local").With("plugin", name).With("operation", "unload").Wrap(ErrPluginNotLoaded)
	}

	delete(h.plugins, name)
	return nil
}

// DeliverCommand sends a command to an in-process plugin handler.
func (h *LocalPluginHost) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_command").Wrap(ErrHostClosed)
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_command").Wrap(ErrPluginNotLoaded)
	}

	if p.commandHandler == nil {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_command").Wrap(ErrNoCommandHandler)
	}

	// Create a delivery-scoped proxy that binds the plugin name for EmitEvent identity.
	scoped := &scopedServiceProxy{base: h.proxy, pluginName: name}
	resp, err := p.commandHandler.HandleCommand(ctx, cmd, scoped)
	if err != nil {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_command").Wrap(err)
	}
	return resp, nil
}

// DeliverEvent sends an event to an in-process plugin handler.
func (h *LocalPluginHost) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_event").Wrap(ErrHostClosed)
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_event").Wrap(ErrPluginNotLoaded)
	}

	if p.eventHandler == nil {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_event").Wrap(ErrNoEventHandler)
	}

	// Create a delivery-scoped proxy that binds the plugin name for EmitEvent identity.
	scoped := &scopedServiceProxy{base: h.proxy, pluginName: name}
	emits, err := p.eventHandler.HandleEvent(ctx, event, scoped)
	if err != nil {
		return nil, oops.In("local").With("plugin", name).With("operation", "deliver_event").Wrap(err)
	}
	return emits, nil
}

// Plugins returns names of all loaded plugins.
func (h *LocalPluginHost) Plugins() []string {
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
func (h *LocalPluginHost) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	h.closed = true
	clear(h.plugins)
	return nil
}
