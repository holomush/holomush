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

### `INV-EVENTBUS`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-EVENTBUS-1` | The gateway process MUST NOT import internal/world, internal/access, internal/store, internal/plugin, internal/eventbus, internal/auth/service, or internal/command. | `INV-GW-1` | pending |
| `INV-EVENTBUS-2` | RenderingPublisher.Publish MUST stamp event.Rendering from the verb registry before publishing. | `INV-GW-2` | pending |
| `INV-EVENTBUS-3` | RenderingPublisher.Publish MUST return EMIT_UNKNOWN_VERB when the verb registry has no entry for event.Type. | `INV-GW-3` | pending |
| `INV-EVENTBUS-4` | JetStreamPublisher.Publish MUST copy event.Rendering into the eventbusv1.Event.Rendering proto field before proto.Marshal; round-trip publish + JetStream consume MUST preserve Rendering byte-for-byte. | `INV-GW-3a` | pending |
| `INV-EVENTBUS-5` | RenderingPublisher.Publish MUST return EMIT_VALIDATION_FAILED when protovalidate.Validate(ev) fails on the stamped frame. | `INV-GW-4` | pending |
| `INV-EVENTBUS-6` | Gateway translation (web + telnet) MUST drop events with Rendering == nil, increment holomush_gateway_dropped_nil_rendering_total, and log an error; MUST NOT render fallback. | `INV-GW-5` | pending |
| `INV-EVENTBUS-7` | Every row in events_audit MUST have a non-nil rendering sub-message after a full E2E run. | `INV-GW-6` | pending |
| `INV-EVENTBUS-8` | RenderingMetadata.label MUST be set when format == "speech"; enforced at the proto layer (CEL) and at VerbRegistry.Register. | `INV-GW-7` | pending |
| `INV-EVENTBUS-9` | RenderingMetadata.display_target MUST NOT be EVENT_CHANNEL_UNSPECIFIED; enforced at the proto layer (enum.not_in: [0]). | `INV-GW-8` | pending |
| `INV-EVENTBUS-10` | RenderingMetadata.source_plugin and source_plugin_version MUST be populated. For builtins, source_plugin == "builtin" and source_plugin_version == "host-<binary version>". | `INV-GW-9` | pending |
| `INV-EVENTBUS-11` | The plugin manager MUST require a non-nil VerbRegistry at construction time; a nil registry returns ErrMissingVerbRegistry. | `INV-GW-10` | pending |
| `INV-EVENTBUS-12` | BootstrapVerbRegistry() MUST be the only public path that returns a registry seeded with host builtins; RegisterBuiltinTypes MUST be unexported. | `INV-GW-11` | pending |
| `INV-EVENTBUS-13` | The audit projection writer MUST read the App-Rendering NATS header and write its JSON value into events_audit.rendering (NOT NULL); a missing, empty, or malformed JSON header MUST fail the insert. | `INV-GW-13` | pending |
| `INV-EVENTBUS-14` | The Go-side eventbus.RenderingMetadata struct and proto-side corev1.RenderingMetadata MUST stay in sync — same field set, same names. | `INV-GW-14` | pending |
| `INV-EVENTBUS-15` | For every event published through RenderingPublisher, the JSON value of the App-Rendering NATS header MUST encode the same RenderingMetadata as the Rendering field inside the proto envelope — the two transports cannot drift. | `INV-GW-15` | pending |
| `INV-EVENTBUS-16` | corev1.EventChannel and webv1.EventChannel MUST stay in lockstep — same enum values, same names, same numeric assignments. | `INV-GW-16` | pending |
| `INV-EVENTBUS-17` | Colon-style subjects appear only as an ABAC policy-DSL identifier, never as a pub/sub stream name (enforced executably by INV-EVENTBUS-19 + INV-EVENTBUS-22). Spec-only — no standalone code annotation. | `INV-ROPS-1` | pending |
| `INV-EVENTBUS-18` | Unclassifiable stream names are rejected at handler entry with INVALID_ARGUMENT, never routed to a default authorization branch. | `INV-ROPS-2` | pending |
| `INV-EVENTBUS-19` | A CI meta-test asserts no production Go or Lua source contains a colon-style entity-prefix literal (location:, character:, scene:, plugin:, …) as a stream name (the eradication gate; ABAC builders are allowlisted). | `INV-ROPS-3` | pending |
| `INV-EVENTBUS-20` | Producer↔subscriber symmetry: an integration test (real embedded NATS) emits through the production producer path for each migrated stream type and asserts a subscriber built from the production filter constructor receives it. | `INV-ROPS-4` | pending |
| `INV-EVENTBUS-21` | Classifier non-collision: a table-driven unit test over the four internal/grpc classifiers asserts location is public-not-scene, character private-not-scene, scene private-and-scene, and unknown/malformed none. | `INV-ROPS-5` | pending |
| `INV-EVENTBUS-22` | Role split both directions: for the same character ULID, the stream is dot (events.<gid>.character.<id>) and the ABAC subject is colon (character:<id>) — guards against an over-eager sweep migrating the ABAC subject. | `INV-ROPS-6` | pending |
| `INV-EVENTBUS-23` | Temporal floor on every private stream: a late joiner cannot read pre-join history on each private stream type (scope floor applied, not zero-floor). StreamProvider populates resource.location + has_location for dot location streams; absent (not empty-sentinel) for non-location streams. | `INV-ROPS-7` | pending |
| `INV-EVENTBUS-24` | Location-seed authorization survives the dot-form flip: an integration test seeds the engine and asserts a co-located character can emit to and read its own dot-form location stream, and a non-co-located character cannot. | `INV-ROPS-8` | pending |
| `INV-EVENTBUS-25` | Plugin audit tables MUST add dek_ref BIGINT NULL and dek_version INTEGER NULL columns (mirror-events_audit contract); the columns are nullable, and identity-codec rows store NULL on both. | `INV-P7-3` | pending |
| `INV-EVENTBUS-26` | Plugin SDK Layer 2: pluginsdk.AuditRow Go struct fields MUST be 1:1 with pluginauditpb.AuditRow proto fields (id, subject, type, timestamp, actor, codec, payload, dek_ref, dek_version). | `INV-P7-4` | pending |
| `INV-EVENTBUS-27` | Plugin migrations MAY run before or after Phase 7's host migration (no host-side schema change beyond Phases 2–5); the two crypto columns added to plugin tables are nullable and require no new host-side support. | `INV-P7-10` | pending |

### `INV-CLUSTER`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-CLUSTER-1` | KEK rotation issues a cluster-prefixed NATS request-reply cache-invalidate ping and MUST receive N-of-N replica acks (30s timeout; rollback on timeout). | `INV-28` | pending |
| `INV-CLUSTER-2` | Rotate/Rekey(context) issues a cluster-prefixed cache-invalidate ping and MUST receive N-of-N replica acks before returning (5s timeout; N=1 degenerates to local self-ack; rollback on timeout). | `INV-29` | pending |
| `INV-CLUSTER-3` | Every cluster.Registry member has a unique MemberID; colliding concurrent registration is rejected with CLUSTER_MEMBER_DUPLICATE_ID. | `INV-53` | pending |
| `INV-CLUSTER-4` | All Phase-3c internal coordination subjects are cluster_id-prefixed; members drop messages whose payload cluster_id disagrees with their configured cluster_id. | `INV-54` | pending |
| `INV-CLUSTER-5` | A pill on internal.<cluster_id>.member.poison.<self_id> triggers Pill.Trigger after flushing audit telemetry; the production Pill terminates the process with exit code 125. | `INV-55` | pending |
| `INV-CLUSTER-6` | invalidation.Coordinator attempts at most one probe-and-pill + retry cycle per RequestInvalidation; after the second timeout it returns INVALIDATION_PARTIAL_FAILURE with the missing-member set. | `INV-56` | pending |
| `INV-CLUSTER-7` | cluster.Registry.ProbeAndPill issues at most one attempt per (member_id, reason) per PillRateLimit window (claim-then-probe, closing the TOCTOU race); over-limit returns ErrPillRateLimited without reaching the wire. | `INV-57` | pending |
| `INV-CLUSTER-8` | No Phase-3c decision is conditioned on cross-host wall-clock comparison (enforced by the noremoteclockcompare analyzer; observability-only skew/latency metrics are the carved-out exceptions). | `INV-58` | pending |
| `INV-CLUSTER-9` | A successful RequestInvalidation(participants_changed) leaves every other live member's dek.ParticipantsCache with no entry for (ctxType, ctxId, version) on return (re-fetch from PG). | `INV-59` | pending |
| `INV-CLUSTER-10` | cluster.Registry.ProbeAndPill refuses id==Self() with ErrCannotPillSelf; the Coordinator filters Self() out of the missing-member set (prevents N=1 self-pill). | `INV-60` | pending |

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
