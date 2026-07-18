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
//
// D-12 Wave B split the former single-phase Start into two phases —
// Prepare and Activate — so the interface itself enforces
// acquire-before-serve rather than relying on every subsystem author
// remembering to declare the right DependsOn edge (D-11's motivating bug:
// grpcSubsystem.DependsOn() once excluded AuditProjection, so gRPC could
// serve before the audit projection was up). The Orchestrator runs Prepare
// over every subsystem in topological order, then Activate over every
// subsystem in topological order — a global barrier between the two
// sweeps. No DependsOn edge is required to get that barrier; it is
// structural.
type Subsystem interface {
	// ID returns the typed identifier for this subsystem.
	ID() SubsystemID

	// DependsOn returns the subsystems that must be started before this one.
	DependsOn() []SubsystemID

	// Prepare acquires and wires everything the subsystem needs, including
	// bringing up process-internal substrate that other subsystems must
	// acquire against (e.g. the embedded NATS server, the DB pool,
	// JetStream streams/durable consumers, plugin processes). Prepare MUST
	// NOT expose any DOMAIN surface reachable from outside the process, and
	// MUST NOT start a work loop that carries domain traffic.
	//
	// The boundary is drawn at "externally reachable domain surface" rather
	// than "anything running" because process-internal substrate is
	// acquisition, not serving: the embedded NATS server sets
	// DontListen: true (internal/eventbus/subsystem.go) and binds no
	// socket, yet the audit subsystem's own Prepare requires that server
	// live (it fails closed with AUDIT_DEP_NOT_STARTED otherwise) — a
	// barrier that forbade any process-internal substrate from coming up in
	// Prepare would be circular.
	//
	// Exception 1 (observability): an OBSERVABILITY surface — e.g. the NATS
	// Prometheus monitor's operator-facing HTTP port
	// (internal/eventbus/subsystem.go, HTTPPort: s.cfg.MonitorPort) — MAY
	// bind during Prepare. It is not domain traffic, and it is the same
	// class as the observability server that already starts before
	// StartAll today. This is the only class of externally-reachable bind
	// permitted in Prepare; a DOMAIN surface (a gRPC listener, admin.sock)
	// binding in Prepare violates this contract.
	//
	// Exception 2 (plugin subprocess launch): PluginSubsystem's Prepare
	// launches plugin subprocesses. That launch is host-controlled
	// acquisition (a child process reachable only over the host's own
	// mTLS gRPC, no externally-reachable surface) — but what a plugin does
	// inside its own Init is outside the orchestrator's enforcement. This
	// interface's guarantee applies to host-owned Activate bodies; it does
	// not cover plugin-side behavior.
	//
	// Prepare MUST be idempotent: a subsystem may guard a non-idempotent
	// side effect (launching a goroutine, binding a listener) because the
	// orchestrator does not promise to call Prepare exactly once — a failed
	// Activate elsewhere in the sweep rolls back via Stop, and a caller may
	// legitimately retry Prepare after fixing a transient failure.
	//
	// A non-nil error is fatal — the server will not start, and Stop is
	// called on this subsystem (and every previously-prepared one) during
	// rollback, because a failed Prepare may have partially acquired
	// resources.
	Prepare(ctx context.Context) error

	// Activate begins serving: it binds/accepts externally-reachable
	// connections and starts work loops that carry domain traffic
	// (delivering events, dispatching commands, sweeping, publishing).
	// Activate MUST assume every subsystem — including this one — has
	// already Prepared; the Orchestrator runs the whole Prepare sweep
	// before the whole Activate sweep, so no Activate observes a
	// not-yet-prepared peer.
	//
	// A subsystem that serves nothing implements Activate as a documented
	// no-op returning nil — that is a decided, correct disposition for many
	// subsystems, not a smell.
	//
	// Activate MUST be idempotent, for the same reason as Prepare: the
	// orchestrator does not promise exactly-once invocation under rollback.
	//
	// A non-nil error is fatal — the server will not start, and Stop is
	// called on every prepared subsystem (a superset of the activated set)
	// during rollback.
	Activate(ctx context.Context) error

	// Stop shuts down the subsystem. It is the single teardown path for a
	// subsystem in any of three states: prepared-but-never-activated,
	// partially prepared (Prepare itself failed partway through), and fully
	// activated. Stop MUST be safe to call in all three states, MUST be
	// idempotent, and MUST NOT block indefinitely.
	Stop(ctx context.Context) error
}
