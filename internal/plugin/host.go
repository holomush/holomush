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
