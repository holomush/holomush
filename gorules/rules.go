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

// dekMaterialPath is the fully-qualified type path the matcher tests
// against. Updating this requires updating the dek package import path.
//
// The rules below (DEKMaterialNo* and CodecKeyBytesAllowlist) implement
// INV-27 sink-side enforcement for the event-payload-crypto Phase 2
// substrate. They were originally drafted as separate files
// (gorules/dek_no_serialize.go, gorules/codec_key_bytes_allowlist.go)
// per the Phase 2 plan, but golangci-lint v2.11's gocritic ruleguard
// configuration silently fails to load multi-file rule sets via the
// `rules:` comma-separated path syntax. The fallback documented in the
// plan is to concatenate into this single rules.go file. Documentation
// fixtures remain in gorules/testdata/ for reviewer reference.
//
// Acceptance for Phase 2 Task 10 is the practical one (per plan):
// rules compile under the `ruleguard` build tag, real production code
// passes `task lint` without false positives, and the testdata
// documentation files give reviewers a clear picture of intent.
// A known go-ruleguard execution-time quirk: when a rule's
// `Type.Is("…/some/Type")` references a type whose package is also
// being scanned, ruleguard may surface an "empty rule set" execution
// error during the smoke-test scenario where the offending pattern is
// added to that very package. The static API surface test in
// internal/eventbus/crypto/dek/api_test.go is the ground-truth defense
// for the Material non-leakage invariant; these ruleguard rules are
// the reviewer-facing supplemental gate.
const dekMaterialPath = "github.com/holomush/holomush/internal/eventbus/crypto/dek.Material"

// DEKMaterialNoJSON forbids passing dek.Material to encoding/json.
// INV-27 sink-side enforcement.
func DEKMaterialNoJSON(m dsl.Matcher) {
	m.Match(`json.Marshal($x)`, `json.MarshalIndent($x, $_, $_)`).
		Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
		Report(`INV-27: dek.Material MUST NOT be passed to encoding/json. ` +
			`Material is opaque DEK material; serializing it leaks unwrapped bytes.`)
}

// DEKMaterialNoGob forbids passing dek.Material to encoding/gob.
func DEKMaterialNoGob(m dsl.Matcher) {
	m.Match(`gob.NewEncoder($_).Encode($x)`).
		Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
		Report(`INV-27: dek.Material MUST NOT be passed to encoding/gob.`)
}

// DEKMaterialNoProto forbids passing dek.Material to proto.Marshal.
func DEKMaterialNoProto(m dsl.Matcher) {
	m.Match(`proto.Marshal($x)`).
		Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
		Report(`INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto.Marshal.`)
}

// DEKMaterialNoFmtFormatting forbids passing dek.Material to fmt
// formatting functions. Per master spec §"Note on variadic patterns"
// in the design notes, ruleguard cannot type-filter variadic `$*xs`
// captures (they bind to multi-node groups with no .Type). We enumerate
// patterns per-arg-count for the practical 1- and 2-argument cases.
// This covers the realistic Material-leak shape — a single Material
// passed alongside a format string. Multi-arg-with-Material-elsewhere
// callsites are left to the static API surface test in
// internal/eventbus/crypto/dek/api_test.go.
func DEKMaterialNoFmtFormatting(m dsl.Matcher) {
	matches := []string{
		`fmt.Sprint($x)`,
		`fmt.Sprintf($_, $x)`,
		`fmt.Sprintln($x)`,
		`fmt.Print($x)`,
		`fmt.Printf($_, $x)`,
		`fmt.Println($x)`,
		`fmt.Fprint($_, $x)`,
		`fmt.Fprintf($_, $_, $x)`,
		`fmt.Fprintln($_, $x)`,
		`fmt.Errorf($_, $x)`,
	}
	for _, p := range matches {
		m.Match(p).
			Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
			Report(`INV-27: dek.Material MUST NOT be passed to fmt formatting/print functions. ` +
				`Material's GoString/Stringer-default would dump bytes; if you need to log a ` +
				`DEK reference, log codec.KeyID instead.`)
	}
}

// DEKMaterialNoLog forbids passing dek.Material to log functions.
// As with DEKMaterialNoFmtFormatting, single-arg patterns only.
func DEKMaterialNoLog(m dsl.Matcher) {
	matches := []string{
		`log.Print($x)`,
		`log.Printf($_, $x)`,
		`log.Println($x)`,
		`log.Fatal($x)`,
		`log.Fatalf($_, $x)`,
		`log.Fatalln($x)`,
		`log.Panic($x)`,
		`log.Panicf($_, $x)`,
		`log.Panicln($x)`,
	}
	for _, p := range matches {
		m.Match(p).
			Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
			Report(`INV-27: dek.Material MUST NOT be passed to log functions.`)
	}
}

// DEKMaterialNoSlog forbids passing dek.Material to log/slog.
// slog's structured-logging API takes alternating key/value pairs; the
// realistic Material-leak shape is `slog.X("msg", "key", material)`,
// which we match as a 2-trailing-arg single-node pattern. slog.Any
// is the explicit value-attribute constructor.
func DEKMaterialNoSlog(m dsl.Matcher) {
	matches := []string{
		`slog.Info($_, $_, $x)`,
		`slog.Debug($_, $_, $x)`,
		`slog.Warn($_, $_, $x)`,
		`slog.Error($_, $_, $x)`,
		`slog.Any($_, $x)`,
	}
	for _, p := range matches {
		m.Match(p).
			Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
			Report(`INV-27: dek.Material MUST NOT be passed to log/slog functions.`)
	}
}

// Note: arbitrary io.Writer-by-interface and concrete-writer.Write
// patterns are NOT covered by ruleguard. The Write methods on
// os.File, bytes.Buffer, etc., take []byte (not Material), so a
// type-filter pattern like `$_.Write($x)` with $x being Material
// would never match — Go's type system rejects the call before
// ruleguard sees it.
//
// The realistic Material-leak paths the rules above catch are
// reflection-based serializers (json/gob/proto) and stringer-based
// formatters (fmt/log/slog). Combined with the static API surface
// test (no exported []byte from the dek package), these defenses
// cover the practical exfiltration surface.

// CodecKeyBytesAllowlist forbids reads of codec.Key.Bytes outside the
// allowed package set. Master-spec amendment per Phase 2 design notes:
// tightens the master spec's "Kept (semantics unchanged)" classification
// for codec.Key by restricting WHO may read the field.
//
// Allowed package paths:
//   - internal/eventbus/codec/...   (codec implementations)
//   - internal/eventbus/crypto/...  (substrate construction + tests)
//
// This is the residual-defense rule for INV-27. dek.Material is opaque
// (no exported []byte accessor), but its AsCodecKey returns a
// codec.Key whose Bytes field is publicly readable. This rule gates
// who may read it.
func CodecKeyBytesAllowlist(m dsl.Matcher) {
	const codecKey = "github.com/holomush/holomush/internal/eventbus/codec.Key"
	const allowed = `^github\.com/holomush/holomush/internal/eventbus/(codec|crypto)(/|$)`
	const msg = `INV-27 (residual defense): codec.Key.Bytes reads are restricted to ` +
		`internal/eventbus/codec/... and internal/eventbus/crypto/.... ` +
		`If you need raw DEK bytes, you are probably in the wrong package — route through ` +
		`dek.Manager or implement a codec.Codec.`

	m.Match(`$x.Bytes`).
		Where(m["x"].Type.Is(codecKey) && !m.File().PkgPath.Matches(allowed)).
		Report(msg)
}
