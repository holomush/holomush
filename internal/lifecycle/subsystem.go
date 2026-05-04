// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package lifecycle provides subsystem lifecycle management, health tracking,
// and readiness gating for the core server.
package lifecycle

import "context"

//go:generate stringer -type=SubsystemID -linecomment

// SubsystemID is a compile-time-safe typed identifier for server subsystems.
type SubsystemID int

// SubsystemID constants enumerate all registered server subsystems.
const (
	SubsystemDatabase        SubsystemID = iota // database
	SubsystemTLS                                // tls
	SubsystemABAC                               // abac
	SubsystemAuth                               // auth
	SubsystemWorld                              // world
	SubsystemPlugins                            // plugins
	SubsystemSessions                           // sessions
	SubsystemBootstrap                          // bootstrap
	SubsystemGRPC                               // grpc
	SubsystemEventBus                           // eventbus
	SubsystemAuditProjection                    // audit_projection
	SubsystemCluster                            // cluster
)

// Subsystem is a top-level server component with lifecycle management
// and dependency declaration.
type Subsystem interface {
	// ID returns the typed identifier for this subsystem.
	ID() SubsystemID

	// DependsOn returns the subsystems that must be started before this one.
	DependsOn() []SubsystemID

	// Start initializes the subsystem. It MUST be idempotent.
	// A non-nil error is fatal — the server will not start.
	Start(ctx context.Context) error

	// Stop shuts down the subsystem. It MUST be idempotent and
	// MUST NOT block indefinitely.
	Stop(ctx context.Context) error
}
