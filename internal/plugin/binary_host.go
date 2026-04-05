// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"sync"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Sentinel errors for BinaryPluginHost.
var (
	// ErrNotBinaryPlugin is returned when Load is called with a non-binary manifest.
	ErrNotBinaryPlugin = errors.New("BinaryPluginHost only accepts binary plugins")
	// ErrMissingBinaryConfig is returned when the manifest has no BinaryPlugin config.
	ErrMissingBinaryConfig = errors.New("manifest missing binary-plugin configuration")
)

// Compile-time interface check.
var _ Host = (*BinaryPluginHost)(nil)

// BinaryClientProtocol wraps a go-plugin ClientProtocol and provides a Kill method.
// This interface exists to decouple BinaryPluginHost from os/exec, keeping subprocess
// creation in the caller-supplied BinaryClientFactory.
type BinaryClientProtocol interface {
	// Client returns the gRPC client protocol for dispensing plugin interfaces.
	Client() (hashiplug.ClientProtocol, error)
	// Kill terminates the plugin subprocess.
	Kill()
}

// BinaryClientFactory creates plugin clients for binary plugin subprocesses.
// Implementations are responsible for launching and connecting to the subprocess.
// The production implementation lives in internal/plugin/goplugin to keep os/exec
// usage out of this package.
type BinaryClientFactory interface {
	// NewClient launches a subprocess for the plugin at execPath and returns a
	// BinaryClientProtocol that can dispense a PluginServiceClient.
	NewClient(execPath string) BinaryClientProtocol
}

// BinaryHostConfig configures the BinaryPluginHost.
type BinaryHostConfig struct {
	// ClientFactory creates plugin clients. Required; NewBinaryPluginHost panics if nil.
	ClientFactory BinaryClientFactory

	// Registry is the service registry for future service injection. May be nil.
	Registry *ServiceRegistry
}

// binaryLoadedPlugin holds state for a single loaded binary plugin subprocess.
type binaryLoadedPlugin struct {
	manifest *Manifest
	client   pluginv1.PluginServiceClient
	kill     func()
}

// BinaryPluginHost manages binary plugins via hashicorp/go-plugin subprocesses.
// It communicates with plugins over gRPC using the pluginv1.PluginService proto.
// Subprocess creation is delegated to BinaryClientFactory, keeping os/exec usage
// out of this package.
type BinaryPluginHost struct {
	cfg     BinaryHostConfig
	mu      sync.RWMutex
	plugins map[string]*binaryLoadedPlugin
	closed  bool
}

// NewBinaryPluginHost creates a new BinaryPluginHost.
// Panics if cfg.ClientFactory is nil.
func NewBinaryPluginHost(cfg BinaryHostConfig) *BinaryPluginHost {
	if cfg.ClientFactory == nil {
		panic("plugins: BinaryHostConfig.ClientFactory cannot be nil")
	}
	return &BinaryPluginHost{
		cfg:     cfg,
		plugins: make(map[string]*binaryLoadedPlugin),
	}
}

// Load starts a binary plugin subprocess for the given manifest.
// The manifest must have Type == TypeBinary and a non-nil BinaryPlugin config.
// dir is passed to the ClientFactory to resolve the executable path.
func (h *BinaryPluginHost) Load(ctx context.Context, manifest *Manifest, dir string) error {
	if err := ctx.Err(); err != nil {
		return oops.In("binary").With("operation", "load").Wrap(err)
	}

	if manifest == nil {
		return oops.In("binary").With("operation", "load").New("manifest cannot be nil")
	}

	if manifest.Type != TypeBinary {
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("type", manifest.Type).
			With("operation", "load").
			Wrap(ErrNotBinaryPlugin)
	}

	if manifest.BinaryPlugin == nil {
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "load").
			Wrap(ErrMissingBinaryConfig)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "load").
			Wrap(ErrHostClosed)
	}

	if _, ok := h.plugins[manifest.Name]; ok {
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "load").
			New("plugin already loaded")
	}

	client := h.cfg.ClientFactory.NewClient(binaryExecPath(dir, manifest.BinaryPlugin.Executable))

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "connect").
			Wrap(err)
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "dispense").
			Wrap(err)
	}

	svcClient, ok := raw.(pluginv1.PluginServiceClient)
	if !ok {
		client.Kill()
		return oops.In("binary").
			With("plugin", manifest.Name).
			With("operation", "load").
			New("dispensed plugin does not implement PluginServiceClient")
	}

	h.plugins[manifest.Name] = &binaryLoadedPlugin{
		manifest: manifest,
		client:   svcClient,
		kill:     client.Kill,
	}

	return nil
}

// Unload kills the subprocess for the named plugin and removes it from the host.
func (h *BinaryPluginHost) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("binary").With("plugin", name).With("operation", "unload").Wrap(ErrHostClosed)
	}

	p, ok := h.plugins[name]
	if !ok {
		return oops.In("binary").With("plugin", name).With("operation", "unload").Wrap(ErrPluginNotLoaded)
	}

	p.kill()
	delete(h.plugins, name)
	return nil
}

// DeliverCommand sends a command to the named binary plugin via gRPC.
func (h *BinaryPluginHost) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_command").Wrap(ErrHostClosed)
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_command").Wrap(ErrPluginNotLoaded)
	}

	resp, err := p.client.HandleCommand(ctx, &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       cmd.Command,
			Args:          cmd.Args,
			RawInput:      cmd.InvokedAs,
			CharacterId:   cmd.CharacterID,
			CharacterName: cmd.CharacterName,
			LocationId:    cmd.LocationID,
			SessionId:     cmd.SessionID,
			PlayerId:      cmd.PlayerID,
		},
	})
	if err != nil {
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_command").Wrap(err)
	}

	return binaryProtoCommandResponseToSDK(resp.GetResponse()), nil
}

// DeliverEvent sends an event to the named binary plugin via gRPC and returns
// any emit events the plugin produces.
func (h *BinaryPluginHost) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_event").Wrap(ErrHostClosed)
	}
	p, ok := h.plugins[name]
	h.mu.RUnlock()

	if !ok {
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_event").Wrap(ErrPluginNotLoaded)
	}

	resp, err := p.client.HandleEvent(ctx, &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id:        event.ID,
			Stream:    event.Stream,
			Type:      string(event.Type),
			Timestamp: event.Timestamp,
			ActorKind: event.ActorKind.String(),
			ActorId:   event.ActorID,
			Payload:   event.Payload,
		},
	})
	if err != nil {
		return nil, oops.In("binary").With("plugin", name).With("operation", "deliver_event").Wrap(err)
	}

	protoEmits := resp.GetEmitEvents()
	if len(protoEmits) == 0 {
		return nil, nil
	}

	emits := make([]pluginsdk.EmitEvent, len(protoEmits))
	for i, e := range protoEmits {
		emits[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}
	return emits, nil
}

// Plugins returns the names of all currently loaded binary plugins.
func (h *BinaryPluginHost) Plugins() []string {
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

// Close kills all plugin subprocesses and marks the host as closed.
// Subsequent operations return ErrHostClosed. Close is idempotent.
func (h *BinaryPluginHost) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	for _, p := range h.plugins {
		p.kill()
	}

	h.closed = true
	clear(h.plugins)
	return nil
}

// --- binaryGRPCPlugin implements hashiplug.GRPCPlugin for the host side ---

// binaryGRPCPlugin is the host-side go-plugin registration. GRPCClient returns
// a PluginServiceClient; GRPCServer is never called on the host side.
type binaryGRPCPlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
}

// GRPCClient returns a PluginServiceClient wrapping the provided gRPC connection.
func (p *binaryGRPCPlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, cc *grpc.ClientConn) (interface{}, error) {
	return pluginv1.NewPluginServiceClient(cc), nil
}

// GRPCServer is not used on the host side.
func (p *binaryGRPCPlugin) GRPCServer(_ *hashiplug.GRPCBroker, _ *grpc.Server) error {
	return errors.New("binary: GRPCServer not implemented on host side")
}

// --- conversion helpers ---

// binaryProtoCommandResponseToSDK converts a proto CommandResponse to the SDK type.
func binaryProtoCommandResponseToSDK(r *pluginv1.CommandResponse) *pluginsdk.CommandResponse {
	if r == nil {
		return &pluginsdk.CommandResponse{}
	}

	events := make([]pluginsdk.EmitEvent, len(r.GetEvents()))
	for i, e := range r.GetEvents() {
		events[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}

	return &pluginsdk.CommandResponse{
		Status: binaryProtoCommandStatusToSDK(r.GetStatus()),
		Output: r.GetOutput(),
		Events: events,
	}
}

// binaryProtoCommandStatusToSDK converts a proto CommandStatus to the SDK type.
func binaryProtoCommandStatusToSDK(s pluginv1.CommandStatus) pluginsdk.CommandStatus {
	switch s {
	case pluginv1.CommandStatus_COMMAND_STATUS_OK:
		return pluginsdk.CommandOK
	case pluginv1.CommandStatus_COMMAND_STATUS_ERROR:
		return pluginsdk.CommandError
	case pluginv1.CommandStatus_COMMAND_STATUS_FAILURE:
		return pluginsdk.CommandFailure
	case pluginv1.CommandStatus_COMMAND_STATUS_FATAL:
		return pluginsdk.CommandFatal
	default:
		return pluginsdk.CommandOK
	}
}

// binaryExecPath joins a base directory and relative executable path.
// Path validation (traversal checks, permissions) is the responsibility of the
// BinaryClientFactory implementation.
func binaryExecPath(dir, executable string) string {
	return dir + "/" + executable
}
