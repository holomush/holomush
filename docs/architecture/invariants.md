<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Invariant Registry

Canonical registry of all named system invariants. Paired with
`invariants.yaml` (machine-readable source of truth). The meta-test at
`test/meta/invariant_registry_test.go` reads the YAML file directly.

**This document is generated** inside the `BEGIN GENERATED` / `END GENERATED`
regions below: `cmd/inv-render` renders them from `invariants.yaml`. Do not
hand-edit inside those regions — edit the YAML and run `task invariants:render`.
The prose outside the regions is hand-authored. CI runs `inv-render -check`
(generate-and-diff) and fails if the rendered regions drift from the YAML.

## Scope index

<!-- BEGIN GENERATED: scope-index (edit invariants.yaml + run `task invariants:render`) -->

| Scope | Description | Boundary |
|-------|-------------|----------|
| `INV-CRYPTO` | Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream | Cryptographic operations on event payloads. Does NOT include: audit projection (→ INV-EVENTBUS), plugin manifest validation (→ INV-PLUGIN), cluster coordination (→ INV-CLUSTER). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under INV-CLUSTER. |
| `INV-PRIVACY` | Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics | Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS). |
| `INV-PRESENCE` | Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration | Current-state presence queries. Does NOT include: session status lifecycle (→ INV-SESSION). |
| `INV-SCENE` | Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability | All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8). |
| `INV-PLUGIN` | Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz | Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ INV-CRYPTO). |
| `INV-EVENTBUS` | Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication | Event infrastructure. Does NOT include: event payload encryption (→ INV-CRYPTO), history privacy gating (→ INV-PRIVACY). |
| `INV-CLUSTER` | Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence | Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ INV-CRYPTO). |
| `INV-ACCESS` | ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions | Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS). |
| `INV-SESSION` | Session status lifecycle, connection attachment, focus membership, idle detection | Session state machine. Does NOT include: presence snapshot (→ INV-PRESENCE). |
| `INV-STORE` | Migration discipline, no-DELETE enforcement, spec compliance scanning | Database invariants. |
| `INV-TELEMETRY` | Logging discipline, trace context, metric naming, sloglint policy | Observability contracts. |
| `INV-BRANDING` | Asset integrity, palette tokens, logo generation | Visual identity invariants. Does NOT include: docs quality (separate concern). |
| `INV-DOCS` | Proto doc comments, doc IA, contributor onboarding surface | Documentation quality invariants. |

<!-- END GENERATED: scope-index -->

A new scope is warranted when at least 3 invariants exist that don't fit an
existing scope's boundary, or when a new major subsystem ships with its own
invariants.

## Invariant tables

<!-- BEGIN GENERATED: invariant-tables (edit invariants.yaml + run `task invariants:render`) -->

### `INV-PRIVACY`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-PRIVACY-1` | A session may read only events from the interval its session row has existed for that stream's scope (active/idle/detached-within-TTL); the session-row lifetime is the continuity unit. ABAC read_unrestricted_history grants a limited bypass (location hard-gate only; temporal floor still applies). | `I-PRIV-1` | pending |
| `INV-PRIVACY-2` | Guest sessions get a temporal floor of MAX(scope_floor, guest_character.CreatedAt) on all stream history reads. | `I-PRIV-2` | pending |
| `INV-PRIVACY-3` | Subscribe.ReattachCAS and SelectCharacter reattach leave LocationArrivedAt UNCHANGED and MUST NOT change the durable's DeliverPolicy/OptStartTime/OptStartSeq (FilterSubjects may change). | `I-PRIV-3` | pending |
| `INV-PRIVACY-4` | Idle status change and transport/SelectCharacter reattach MUST NOT advance LocationArrivedAt. | `I-PRIV-4` | pending |
| `INV-PRIVACY-5` | All denial paths (hard-gate, I-17, ABAC, expired/missing session) return the same wire code STREAM_ACCESS_DENIED; the internal denial_reason is slog-only and never crosses the wire. | `I-PRIV-5` | pending |
| `INV-PRIVACY-6` | ABAC staff override bypasses the hard-gate location-match only, NOT the temporal floor. | `I-PRIV-6` | pending |
| `INV-PRIVACY-7` | Plugin-owned subjects with divergent history-replay semantics MUST declare history_scope in the manifest and be exercised by a test; silent inheritance of permissive semantics is forbidden. | `I-PRIV-7` | pending |
| `INV-PRIVACY-8` | OpenSession (incl. reattach) and SetFilters query the existing durable before CreateOrUpdateConsumer; an existing durable's DeliverPolicy/OptStartTime/OptStartSeq are copied verbatim (only FilterSubjects mutates); NATS is the source of truth. | `I-PRIV-8` | pending |

### `INV-PRESENCE`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-PRESENCE-1` | Snapshot returns only Active sessions; Detached/Expired excluded. | `I-PRES-1` | pending |
| `INV-PRESENCE-2` | Snapshot exempt from I-PRIV-1 temporal floor (timeless current state). | `I-PRES-2` | pending |
| `INV-PRESENCE-3` | Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe). | `I-PRES-3` | pending |
| `INV-PRESENCE-4` | RPC ABAC-gated by action=list_presence on resource=location:<id>. | `I-PRES-4` | pending |
| `INV-PRESENCE-5` | Non-empty FocusMemberships → UNIMPLEMENTED; no silent fallback. | `I-PRES-5` | pending |
| `INV-PRESENCE-6` | Caller's own session included when status+location qualify. | `I-PRES-6` | pending |
| `INV-PRESENCE-7` | PresenceEntry has exactly 3 fields: character_id, character_name, state. | `I-PRES-7` | pending |
| `INV-PRESENCE-8` | Client presence map keyed by character_id; idempotent add/remove. | `I-PRES-8` | pending |
| `INV-PRESENCE-9` | Response deduplicates by character_id (defense-in-depth). | `I-PRES-9` | pending |

### `INV-ACCESS`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-ACCESS-1` | With WithRealABAC(), the CoreServer access engine is the setup.BuildABACStack engine, not allowAllPolicyEngine. | `INV-RA-1` | pending |
| `INV-ACCESS-2` | Without WithRealABAC(), the harness retains the allow-all default (no regression). | `INV-RA-2` | pending |
| `INV-ACCESS-3` | With WithRealABAC(), the seed:* policy set is installed before the engine's cache loads; the engine evaluates against a non-empty seeded policy set. | `INV-RA-3` | pending |
| `INV-ACCESS-4` | With WithRealABAC()+WithInTreePlugins(), the attribute.Resolver and attribute.PluginProvider the plugin subsystem registers on are the SAME instances (pointer identity) the engine evaluates against. | `INV-RA-4` | pending |
| `INV-ACCESS-5` | Every attribute namespace referenced by an installed seed policy has a registered provider under WithRealABAC (no silent default-deny from an unregistered provider). | `INV-RA-5` | pending |
| `INV-ACCESS-6` | Option order MUST NOT affect the resulting stack: Start(t,A,B) and Start(t,B,A) produce identical permit/deny behavior. | `INV-RA-6` | pending |

### `INV-SESSION`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-SESSION-1` | session.Store has exactly one production implementation: store.PostgresSessionStore. | `INV-M-1` | pending |
| `INV-SESSION-2` | sessiontest.NewStore(t) returns a fresh, isolated store per invocation; cross-test state never leaks. | `INV-M-2` | pending |
| `INV-SESSION-3` | PostgresSessionStore.AddConnection rejects invalid client_type (accept terminal/comms_hub/telnet; reject others). | `INV-M-3` | pending |
| `INV-SESSION-4` | Memstore-removal preserves behavioral coverage: every pre-consolidation test is named in a surviving test's // replaces: chain. | `INV-M-4` | pending |

### `INV-STORE`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-STORE-1` | All persistent time values stored as BIGINT epoch-ns (UTC); no new TIMESTAMPTZ/TIMESTAMP columns. | `INV-TS-1` | pending |
| `INV-STORE-2` | pgnanos.Time is the canonical scan/insert seam between time.Time and BIGINT epoch-ns; no int64<->time.Time arithmetic outside pgnanos. | `INV-TS-2` | pending |
| `INV-STORE-3` | Application code (production + tests) must not Truncate(time.Microsecond) on any time.Time round-tripping through PG. | `INV-TS-3` | pending |
| `INV-STORE-4` | publisher.Publish does not truncate event.Timestamp before AAD/envelope; the on-wire timestamp carries full nanosecond precision. | `INV-TS-4` | pending |
| `INV-STORE-5` | AAD round-trip publish->persist->read->reconstruct is byte-equal at full nanosecond resolution (strengthens former INV-P7-16). | `INV-TS-5` | pending |
| `INV-STORE-6` | Privacy/scope floor comparisons operate at nanosecond resolution; the dispatchDelivery Truncate(microsecond) is deleted, not stubbed. | `INV-TS-6` | pending |
| `INV-STORE-7` | Sub-microsecond timestamp ties resolve deterministically; the privacy floor uses >= so an event at the exact floor ns is included. | `INV-TS-7` | pending |
| `INV-STORE-9` | TIMESTAMPTZ->BIGINT conversion migrations saturate out-of-range / +/-infinity to int64 bounds, pass NULL through, and convert in-range values exactly (numeric arithmetic). | `INV-TS-9` | pending |

### `INV-TELEMETRY`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-TELEMETRY-1` | Load harness drives the web tier over the Connect protocol (not gRPC/gRPC-Web). | `INV-LOAD-1` | pending |
| `INV-TELEMETRY-2` | Load harness drives the telnet tier over raw TCP through the real gateway telnet listener. | `INV-LOAD-2` | pending |
| `INV-TELEMETRY-3` | say->broadcast and page/whisper->delivery latency is computed from an in-payload emit-timestamp recorded by the recipient VU (never cross-VU shared state); generator/SUT clock skew <= 50ms. | `INV-LOAD-3` | pending |
| `INV-TELEMETRY-4` | Load pass/fail verdict is derived from k6's exit code (thresholds), never a substring match on k6 output. | `INV-LOAD-4` | pending |
| `INV-TELEMETRY-5` | SLO thresholds gate against .benchmarks/load-baseline.json (relative regression), not hard-coded absolutes, once a baseline exists. | `INV-LOAD-5` | pending |
| `INV-TELEMETRY-6` | Load scenario must not issue command verbs not registered in the running server (command-availability gating). | `INV-LOAD-6` | pending |
| `INV-TELEMETRY-7` | Load action selection is seeded deterministically so two runs of the same scenario config produce the same action sequence. | `INV-LOAD-7` | pending |
| `INV-TELEMETRY-8` | The load harness must not be wired into task pr-prep (fast lane). | `INV-LOAD-8` | pending |

<!-- END GENERATED: invariant-tables -->
