// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// WithExtraPluginDir stages an additional plugin directory (e.g. a test-only
// Lua fixture under test/integration/.../testdata/lua/<name>) into the plugin
// load path so the real plugin subsystem loads it alongside the in-tree
// plugins. Used by focus runtime-symmetry tests that need a Lua plugin which
// calls the auto_focus_on_join hostfunc. dir is resolved relative to the test's
// package directory (Go runs tests with CWD = package dir).
func WithExtraPluginDir(dir string) StartOption {
	return func(c *startConfig) { c.extraPluginDirs = append(c.extraPluginDirs, dir) }
}

// WithExternalNATS swaps the harness's default embedded eventbustest bus for a
// production external-mode eventbus.Subsystem that dials url. This is the seam
// the two-replica world-model resilience suite (OPS-05, #4791) uses so that N
// in-process CoreServer replicas share ONE real NATS JetStream broker instead
// of N isolated in-memory servers.
//
// REQUIRES a running JetStream broker at url — normally an
// internal/testsupport/natstest container (natstest.StartNATS). It is safe for
// multiple replicas to Start against one broker because the subsystem provisions
// EVENTS via CreateOrUpdateStream with a config derived purely from Defaults()
// (see internal/eventbus/subsystem.go EnsureStream/desiredStreamConfig): every
// replica presents an identical desiredStreamConfig, so the second and later
// boots are idempotent no-ops rather than a config-mismatch failure.
//
// Zero blast radius: the field defaults empty and the external branch is taken
// only when set, so every existing suite keeps the byte-for-byte embedded path.
func WithExternalNATS(url string) StartOption {
	return func(c *startConfig) { c.externalNATSURL = url }
}

// WithSharedDatabase joins an existing per-test Postgres database (addressed by
// connStr) instead of creating a fresh one via testutil.FreshDatabase. It is the
// seam the two-replica resilience suite (D-03, #4791) uses so replica 2 boots
// against the SAME database replica 1 created — the precondition for
// characterizing last-write-wins and dual-write behavior across replicas.
//
// A second Start on a shared database re-seeds its own guest start location
// (benign — distinct ULIDs, so no unique-key collision) and re-runs the
// versioned plugin migrations (a no-op on an already-migrated schema). The boot
// KEK env vars are re-pointed at the newest replica's ephemeral keyfile (benign
// while the resilience suite avoids WithPluginCrypto).
//
// Zero blast radius: the field defaults empty and the shared branch is taken
// only when set, so every existing suite keeps the fresh-DB-per-Start path.
func WithSharedDatabase(connStr string) StartOption {
	return func(c *startConfig) { c.sharedConnStr = connStr }
}

// WithCharacterActivity boots the real internal/charactivity subsystem inside
// the harness (D-42, IDENT-10) so a spec can observe the whole
// event → KV buffer → periodic flush → characters.last_active_at path.
//
// storage MUST be jetstream.MemoryStorage in a memory harness. A KV bucket
// carries its OWN storage config and does not inherit the stream's, and
// FileStorage is the ZERO VALUE of jetstream.StorageType — so a bucket left at
// the default is file-backed even here, leaking bucket state into the embedded
// server's StoreDir across runs. That is the whole reason charactivity ships a
// NewSubsystemWithStorage pair, and the reason this option takes the parameter
// rather than choosing for the caller.
//
// flushInterval SHOULD be short (a few hundred milliseconds): production's
// default is five minutes, which is the column's worst-case lag by design.
//
// The subsystem consumes bus events directly, so no outbox relay is needed and
// none is started. Zero blast radius: the branch is taken only when the option
// is passed.
func WithCharacterActivity(storage jetstream.StorageType, flushInterval time.Duration) StartOption {
	return func(c *startConfig) {
		c.characterActivity = true
		c.characterActivityStorage = storage
		c.characterActivityFlushInterval = flushInterval
	}
}

// WithOutboxRelay boots the REAL world outbox relay subsystem
// (world/setup.NewOutboxRelaySubsystem) inside Start, over the harness's pool
// and embedded bus.
//
// Without it a world write commits its outbox row and STOPS there: the row is
// never published, so nothing downstream of the bus ever sees the change. Every
// event-driven consumer of world state — the retirement reactor is the first —
// is therefore unobservable in this harness without this option, which is
// exactly the gap that made "retirement is effective" an assertion rather than
// a demonstration.
//
// The subsystem is driven through its production Prepare/Activate/Stop
// contract, so a spec exercises the same lease acquisition, LISTEN/NOTIFY waker
// and drain loop production runs. A spec MUST NOT publish a synthetic
// world-change event to shortcut it — doing so proves the consumer works while
// leaving the write→publish link, the part that was actually missing, untested.
//
// Orthogonal to WithRetirementReactor: neither implies the other. Zero blast
// radius — the branch is taken only when the option is passed.
func WithOutboxRelay() StartOption {
	return func(c *startConfig) { c.outboxRelay = true }
}

// WithRetirementReactor boots the REAL retirement reactor subsystem
// (internal/retirement, IDENT-04, D-36/D-37/D-38) inside Start with
// production-shaped dependencies drawn from the harness stack: the harness's
// session store, its single world.Service, its shared background-job registry,
// and a durable JetStream consumer on the embedded bus.
//
// Two dependencies are deliberately built fresh rather than reused, and each
// for a reason a spec depends on:
//
//   - a REAL presence.Emitter over the bus publisher, because the harness's own
//     emitter publishes into a no-op publisher and the reactor's leave /
//     session_ended emissions would be invisible;
//   - a move destination location distinct from the guest start location, read
//     back via Server.RetirementStartLocation(), because a destination equal to
//     where characters are seeded would hit the reactor's already-there skip
//     gate and correctly emit no move at all.
//
// WHICH ENGINE YOUR SPEC NEEDS:
//
//   - Observing the FANOUT (session ended, leave, session_ended, move) works
//     under the default allow-all engine. The reactor's world calls still cross
//     the real ABAC chokepoint — they simply pass it trivially, so such a spec
//     proves the fanout and says nothing about authorization.
//   - Asserting the job's INSTANCE FENCE (provenance for aggregate X must not
//     authorize a write to aggregate Y, D-47 / 02.2 D-54) additionally requires
//     WithRealABAC(), which seeds seed:job-retirement-instance-scoped. Liveness
//     comes from this subsystem registering "retirement" into the same registry
//     that option's ABAC subsystem reads, so the two options compose; a denial
//     spec MUST carry its positive control, which is what catches a registry
//     that was never threaded (everything denies, and the control fails).
//
// Pair with WithOutboxRelay() for the end-to-end chain — without the relay the
// character_retired envelope never reaches the reactor's consumer.
func WithRetirementReactor() StartOption {
	return func(c *startConfig) { c.retirementReactor = true }
}
