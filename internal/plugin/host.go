// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"

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
	// AddStream subscribes a session to an additional stream.
	// Returns an error (code SESSION_NOT_FOUND) if the session is not active.
	AddStream(ctx context.Context, sessionID, stream string) error
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
