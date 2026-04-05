// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "google.golang.org/grpc"

// typeServerInternal is used for services provided by the server itself.
const typeServerInternal Type = "server-internal"

// HealthReporter reports the health state of a service provider.
type HealthReporter interface {
	// Healthy returns true if the service is available.
	Healthy() bool
}

// RegisteredService represents a proto service registered in the service registry.
type RegisteredService struct {
	// Name is the fully qualified proto service name (e.g., "holomush.scene.v1.SceneService").
	Name string

	// Conn is the gRPC transport to the service implementation.
	Conn grpc.ClientConnInterface

	// PluginName identifies which plugin provides this service. Empty for server-internal services.
	PluginName string

	// PluginType is the type of plugin providing this service (binary, lua, or server-internal).
	PluginType Type

	// Health reports the provider's health state. May be nil if health checking is not supported.
	Health HealthReporter
}

// IsServerInternal returns true if this service is provided by the server, not a plugin.
func (s *RegisteredService) IsServerInternal() bool {
	return s.PluginType == typeServerInternal
}
