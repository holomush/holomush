// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

// SessionStreamsRequest carries session context for plugin stream contribution queries.
type SessionStreamsRequest struct {
	// CharacterID is the character entering the session.
	CharacterID string
	// PlayerID is the player owning the character.
	PlayerID string
	// SessionID is the active session identifier.
	SessionID string
}

// StreamRegistry allows plugins to modify session stream subscriptions mid-session.
type StreamRegistry interface {
	// AddStream subscribes a session to an additional stream with default FROM_CURSOR replay.
	// Returns an error (code SESSION_NOT_FOUND) if the session is not active.
	AddStream(ctx context.Context, sessionID, stream string) error
	// AddStreamWithMode subscribes with an explicit replay mode (e.g., LIVE_ONLY for channels).
	AddStreamWithMode(ctx context.Context, sessionID, stream string, mode session.ReplayMode) error
	// RemoveStream unsubscribes a session from a stream. Idempotent.
	RemoveStream(ctx context.Context, sessionID, stream string) error
}

// Host manages a specific plugin runtime type.
type Host interface {
	// Load initializes a plugin from its manifest.
	Load(ctx context.Context, manifest *Manifest, dir string) error

	// Unload tears down a pluginsdk.
	Unload(ctx context.Context, name string) error

	// DeliverEvent sends an event to a plugin and returns response events.
	DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error)

	// DeliverCommand sends a command to a plugin and returns the response.
	DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)

	// QuerySessionStreams returns stream names the plugin wants subscribed for a session.
	// Only called for plugins with SessionStreams: true in their manifest.
	// Returns nil if the plugin has no streams to contribute.
	QuerySessionStreams(ctx context.Context, name string, req SessionStreamsRequest) ([]string, error)

	// Plugins returns names of all loaded plugins.
	Plugins() []string

	// Close shuts down the host and all plugins.
	Close(ctx context.Context) error
}

// ServiceConnProvider is an optional interface that Host implementations
// may implement to expose the underlying gRPC connection for a loaded plugin.
// Binary plugin hosts implement this so the manager can register plugin-provided
// services in the ServiceRegistry after loading.
type ServiceConnProvider interface {
	// PluginConn returns the gRPC client connection for the named plugin.
	PluginConn(name string) (grpc.ClientConnInterface, error)
}

// AttributeResolverProvider is an optional interface that Host implementations
// may implement to provide AttributeResolver gRPC clients for loaded plugins.
// Binary plugin hosts implement this to support schema discovery and attribute
// resolution for plugin-owned resource types.
type AttributeResolverProvider interface {
	// AttributeResolverClient returns the AttributeResolver gRPC client for a loaded plugin.
	// Returns nil if the plugin is not loaded or doesn't support attribute resolution.
	AttributeResolverClient(pluginName string) pluginv1.AttributeResolverServiceClient
}

// PluginAuditClientProvider is an optional interface that binary-plugin
// hosts implement so the eventbus/audit per-plugin consumer and history
// router can reach the plugin's PluginAuditService. Returns nil when the
// plugin is not loaded or did not register the service.
type PluginAuditClientProvider interface {
	PluginAuditClient(pluginName string) pluginv1.PluginAuditServiceClient
}

// PluginIntentEmitter routes plugin-owned emit intents through the shared host
// event emission path.
type PluginIntentEmitter interface {
	Emit(ctx context.Context, pluginName string, intent pluginsdk.EmitIntent) error
}

// EventEmitterConfigurer is an optional interface for hosts that need the
// shared plugin event emitter injected after construction.
type EventEmitterConfigurer interface {
	SetEventEmitter(emitter PluginIntentEmitter)
}

// HistoryReader provides read-only replay access for host functions (e.g.
// query_stream_history in Lua plugins). Satisfied by MemoryEventStore in
// unit tests and by a JetStream-backed reader in production.
type HistoryReader interface {
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error)
}

// FocusDepsConfigurer is an optional interface for hosts that need the focus
// coordinator and history reader injected after construction. These dependencies
// are created during gRPC subsystem Start, which runs after plugin loading.
type FocusDepsConfigurer interface {
	SetFocusCoordinator(fc focus.Coordinator)
	SetHistoryReader(hr HistoryReader)
}

// IdentityRegistryConfigurer is implemented by hosts that need an
// IdentityRegistry late-bound after construction. The registry is the
// Manager itself, but Hosts are constructed before Manager.RegisterHost
// returns. Manager.RegisterHost calls SetIdentityRegistry on any Host
// that implements this interface.
type IdentityRegistryConfigurer interface {
	SetIdentityRegistry(reg IdentityRegistry)
}
