// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard
// +build ruleguard

// Package gorules contains custom go-ruleguard rules loaded by gocritic.
//
// These rules enforce project invariants that cannot be expressed via
// standard linters. The file is build-tagged so it never compiles with
// the rest of the project — gocritic loads it via its ruleguard checker
// configured in .golangci.yaml.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// EventIDMustBeMonotonic ensures events are constructed via core.NewEvent(),
// not via raw core.Event{} struct literals. core.NewEvent() assigns the ID
// from core.NewULID() (monotonic-within-millisecond), enforcing invariant
// I-16 from the focus-substrate design spec. Raw struct literals risk using
// idgen.New() or ulid.Make() which produce non-monotonic IDs that silently
// break PostgresEventStore.Replay and cursor CAS advances.
//
// The rule catches ANY core.Event{} literal. The only legitimate construction
// site is core.NewEvent() itself (in internal/core/event.go), which is
// excluded via the Where clause filtering on the file path.
//
// See docs/superpowers/specs/2026-04-11-focus-substrate-design.md section 3.1 I-16.
func EventIDMustBeMonotonic(m dsl.Matcher) {
	m.Match(`core.Event{$*_}`).
		Where(!m.File().Name.Matches(`event\.go$`)).
		Report(`use core.NewEvent() instead of raw core.Event{} literal -- see I-16 in focus-substrate spec`)
}

// ULIDMakeForbidden forbids ulid.Make() in production code. ulid.Make()
// uses math/rand internally, violating the project-wide crypto/rand rule.
// Use idgen.New() for entity IDs or core.NewULID() for event IDs. This
// rule replaces the bash check previously at Taskfile.yaml:346.
func ULIDMakeForbidden(m dsl.Matcher) {
	m.Match(`ulid.Make()`).
		Report(`use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`)
}

// SceneOpsEventsAppendOnly forbids UPDATE/DELETE/TRUNCATE statements
// against the scene_ops_events table. The table is the append-only ops
// journal for the core-scenes plugin (Phase 3 design spec P3.D3, P3.D4).
// The single legitimate writer is recordOpsEventTx in
// plugins/core-scenes/ops_events.go (INSERT only).
//
// Per CLAUDE.md ("MUST NOT use triggers or functions — All logic lives
// in Go; PostgreSQL is storage only"), append-only cannot be enforced
// via a database trigger. This lint rule is the project's chosen
// enforcement mechanism — it runs on every task lint, repo-wide,
// before any code executes.
//
// The match anchors on pgx-style Exec/Query/QueryRow callsites and
// inspects the literal SQL string argument. Matching the call
// expression directly (not an enclosing AssignStmt) makes the rule
// independent of how the result is consumed — bare expression
// statements, blank-LHS assigns, and named-LHS assigns all match.
//
// The Text.Matches regex is case-insensitive and tolerates arbitrary
// whitespace (including newlines in raw string literals) between the
// verb and the table name.
//
// Limitations:
//   - SQL passed via concatenation or a const-by-name will NOT be
//     caught (ruleguard sees only literal expressions at the call
//     site). In practice every SQL in this codebase is a single
//     literal argument.
//   - The Type filter on $tx is intentionally omitted: pgx
//     connection/transaction types are plural (pgx.Tx,
//     *pgxpool.Pool, *pgxpool.Conn, etc.) and method-name plus the
//     table-name regex is specific enough to avoid false positives.
//
// Pattern shape derived from go-ruleguard's dsl v0.3.23 reference
// (dsl.go:314 for Text.Matches).
func SceneOpsEventsAppendOnly(m dsl.Matcher) {
	const forbidden = `(?i)(?:update\s+scene_ops_events|delete\s+from\s+scene_ops_events|truncate(?:\s+table)?\s+scene_ops_events)`
	const msg = `scene_ops_events is append-only (Phase 3 design P3.D3/D4): use a new INSERT via recordOpsEventTx to record corrections instead of UPDATE/DELETE/TRUNCATE`

	m.Match(`$tx.Exec($ctx, $sql, $*args)`).
		Where(m["sql"].Text.Matches(forbidden)).
		Report(msg)

	m.Match(`$tx.Query($ctx, $sql, $*args)`).
		Where(m["sql"].Text.Matches(forbidden)).
		Report(msg)

	m.Match(`$tx.QueryRow($ctx, $sql, $*args)`).
		Where(m["sql"].Text.Matches(forbidden)).
		Report(msg)
}
