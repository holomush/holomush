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
	SubsystemAdminSocket                        // admin_socket
	// SubsystemCryptoChainVerifier is the generalized auditchain.VerifierSubsystem
	// walking all registered chains (policy_set + rekey) at boot time. Declared by
	// sub-epic D; broadened by sub-epic E to cover multiple chains.
	SubsystemCryptoChainVerifier  // crypto_chain_verifier
	SubsystemCryptoPolicy         // crypto_policy
	SubsystemRekeyCheckpointSweep // rekey_checkpoint_sweep
	// SubsystemOutboxRelay is the world-change transactional-outbox relay
	// (MODEL-04, 05-07): the single leased publisher that drains outbox rows to
	// JetStream in strict feed order. DependsOn Database + EventBus.
	SubsystemOutboxRelay // outbox_relay
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
