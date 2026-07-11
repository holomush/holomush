// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

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
