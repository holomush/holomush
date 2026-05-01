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

// ULIDMakeForbidden forbids ulid.Make() in production code. ulid.Make()
// uses math/rand internally, violating the project-wide crypto/rand rule.
// Use idgen.New() for entity IDs or core.NewULID() for event IDs. This
// rule replaces the bash check previously at Taskfile.yaml:346.
func ULIDMakeForbidden(m dsl.Matcher) {
	m.Match(`ulid.Make()`).
		Report(`use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`)
}

// CursorPackageInternal forbids importing internal/eventbus/cursor from
// outside the eventbus/grpc/web/plugin-host package trees. The cursor
// codec is host-internal: plugin authors and external clients MUST NOT
// inspect or construct opaque tokens. Allowed importers:
//
//   - internal/eventbus/...      (the codec's natural home)
//   - internal/grpc/...          (decodes/encodes at the RPC boundary)
//   - internal/web/...           (thin proxy over gRPC)
//   - internal/plugin/goplugin/  (plugin host service wraps plugin cursors)
//   - internal/plugin/hostfunc/  (Lua hostfunc encodes/decodes for Lua plugins)
//
// See docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5.
func CursorPackageInternal(m dsl.Matcher) {
	m.Import("github.com/holomush/holomush/internal/eventbus/cursor")
	const allowed = `^github\.com/holomush/holomush/internal/(eventbus|grpc|web|plugin/goplugin|plugin/hostfunc)(/|$)`
	const msg = `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

	m.Match(`cursor.Encode($*_)`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.Decode($*_)`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.Cursor{$*_}`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.CurrentVersion`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.CurrentEpoch()`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.Owner{$*_}`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.HostCursor{$*_}`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	// OwnerKind discriminator — guard the type and its constants so a
	// non-allowlisted importer can't introspect cursor ownership.
	m.Match(`cursor.OwnerKind`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.OwnerHost`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.OwnerPlugin`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)

	m.Match(`cursor.OwnerUnspecified`).
		Where(!m.File().PkgPath.Matches(allowed)).
		Report(msg)
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

// INV-27 (dek.Material non-leakage) ruleguard enforcement is INTENTIONALLY
// ABSENT. The Phase 2 plan originally called for sink-side ruleguard rules
// (DEKMaterialNoJSON, NoGob, NoProto, NoFmtFormatting, NoLog, NoSlog) plus
// CodecKeyBytesAllowlist. Implementation revealed that go-ruleguard's
// rule loader cannot resolve `Type.Is("…/internal/eventbus/crypto/dek.Material")`
// when the analyzed package is in the same Go module — the resolver fails
// silently and ruleguard returns an empty rule set GLOBALLY (not just for
// the affected packages), causing the spurious
//
//	ruleguard: execution error: used Run() with an empty rule set;
//	forgot to call Load() first?
//
// to fire on every package golangci-lint scans (200+ false positives).
// We are already on the latest of every component (golangci-lint v2.11.4,
// go-ruleguard v0.4.5, go-ruleguard/dsl v0.3.23) — no upstream version
// bump available.
//
// Same-shape bug as holomush#1272 (the plugins/ workaround that was
// removed when the WASM-plugin design went away). Any rule using
// `Type.Is(<project-internal-type>)` exhibits the same failure.
//
// Phase 2 INV-27 enforcement therefore lives entirely in:
//   - internal/eventbus/crypto/dek/material.go — opaque struct, private
//     `bytes` field, no exported []byte accessor
//   - internal/eventbus/crypto/dek/api_test.go — static API surface test
//     using golang.org/x/tools/go/packages, asserts the dek package
//     exports no `[]byte` (function/method/struct field). Catches the
//     realistic failure mode (Material gaining an exported accessor).
//
// The reviewer-facing patterns we WANTED to flag at lint time are
// documented for posterity in gorules/testdata/{dek_no_serialize,
// codec_key_bytes}/expected_violations.go.
//
// Tracked: holomush-46ya (chore(lint): migrate from go-ruleguard DSL to
// standard go/analysis framework). Once that lands the rules can be
// re-introduced as plain Go analyzers without the Type.Is resolver
// limitation.
