<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Central Invariant Registry Design

## Motivation

HoloMUSH invariants are scattered across 62 spec files with at least 30
concurrent naming schemes — bare `INV-N` (crypto master spec), per-phase
families (`INV-A-N`..`INV-F-N`, `INV-P4-N`..`INV-P7-N`), per-spec
families (`INV-RB-N`, `INV-ROPS-N`, `INV-GW-N`, `INV-LOAD-N`,
`INV-TS-N`, `INV-WS-N`, `INV-PC-N`, `INV-FS-N`, `INV-FW-N`,
`INV-SH-N`, `INV-RA-N`), and I-prefix families (`I-PRIV-N`,
`I-PRES-N`). A
contributor finding an invariant ID in code must grep the entire `docs/`
tree to learn what it means. Some invariants have prose definitions in
2+ specs (drift risk). New invariants get added without an index update.

This spec establishes a single canonical registry — one file that
catalogs every named system invariant by ID, with a unified naming
scheme, a scope taxonomy, and a drift-protection meta-test.

## Design

### Placement

`docs/architecture/invariants.md` — contributor-facing rendered view,
derived from the canonical registry source of truth at
`docs/architecture/invariants.yaml` (see "Machine-readable source of
truth" below). A future, lower-priority phase may generate a curated
public-facing subset for `site/docs/reference/invariants.md`.

### Naming scheme

Every invariant gets a canonical ID:

```text
INV-<SCOPE>-<N>
```

- `<SCOPE>` is a short uppercase domain label (see Scope index below).
- `<N>` is a sequential integer within that scope, starting at 1.

Existing names (`I-PRIV-7`, `INV-39`, `INV-P7-15`) become legacy aliases
recorded in the registry. All references in code (`// Verifies:`),
specs, and test names MUST be renamed to the canonical form as part of
the cataloging pass.

### Scope index

The registry file opens with a scope index — one row per domain with a
prose description and explicit boundaries. This serves as the bucketing
guide for future contributors: where does a new invariant go, and when
does a new scope get created?

| Scope | Description | Boundary |
|-------|-------------|----------|
| `INV-CRYPTO` | Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream | Cryptographic operations on event payloads. Does NOT include: audit projection (→ `INV-EVENTBUS`), plugin manifest validation (→ `INV-PLUGIN`), cluster coordination (→ `INV-CLUSTER`). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under `INV-CLUSTER`. |
| `INV-PRIVACY` | Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics | Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ `INV-ACCESS`), subscribe authorization (→ `INV-EVENTBUS`). |
| `INV-PRESENCE` | Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration | Current-state presence queries. Does NOT include: session status lifecycle (→ `INV-SESSION`). |
| `INV-SCENE` | Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability | All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8). |
| `INV-PLUGIN` | Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz | Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ `INV-CRYPTO`). |
| `INV-EVENTBUS` | Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication | Event infrastructure. Does NOT include: event payload encryption (→ `INV-CRYPTO`), history privacy gating (→ `INV-PRIVACY`). |
| `INV-CLUSTER` | Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence | Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ `INV-CRYPTO`). |
| `INV-ACCESS` | ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions | Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ `INV-EVENTBUS`). |
| `INV-SESSION` | Session status lifecycle, connection attachment, focus membership, idle detection | Session state machine. Does NOT include: presence snapshot (→ `INV-PRESENCE`). |
| `INV-STORE` | Migration discipline, no-DELETE enforcement, spec compliance scanning | Database invariants. |
| `INV-TELEMETRY` | Logging discipline, trace context, metric naming, sloglint policy | Observability contracts. |
| `INV-BRANDING` | Asset integrity, palette tokens, logo generation | Visual identity invariants. Does NOT include: docs quality (separate concern). |
| `INV-DOCS` | Proto doc comments, doc IA, contributor onboarding surface | Documentation quality invariants. |

A new scope is warranted when at least 3 invariants exist that don't fit
an existing scope's boundary, or when a new major subsystem ships with
its own invariants.

### Invariant table

Each scope section contains a markdown table:

| ID | Legacy | Summary | Severity | Status | Asserted by | Reviewed |
|----|--------|---------|----------|--------|-------------|----------|
| `INV-CRYPTO-1` | `INV-1` | Operator MUST NOT see plaintext via live JetStream sub | MUST | active | `test/integration/crypto/metadata_only_test.go` | — |

Columns:

- **ID** — canonical `INV-<SCOPE>-<N>`.
- **Legacy** — comma-separated prior IDs.
- **Summary** — one-line prose. Uses RFC2119 keywords where the
  invariant's severity is MUST/SHOULD.
- **Severity** — `MUST` or `SHOULD` (RFC2119). `PRIVACY-LOAD-BEARING`
  tag MAY be appended for invariants where a violation is a privacy
  incident.
- **Status** — `active`, `superseded-by-<ID>`, or `retired`.
- **Asserted by** — Go test function names or Ginkgo `Describe`/`It`
  paths that carry a `// Verifies: INV-<SCOPE>-<N>` annotation. MAY
  reference a static analyzer path (e.g.,
  `gorules/analyzers/noremoteclockcompare/`) for lint-enforced
  invariants.
- **Reviewed** — date and reviewer of last manual review.

### Drift-protection meta-test

A single Go test at `test/meta/invariant_registry_test.go` replaces the
current per-family hardcoded slices (`i_priv_coverage_test.go`,
`inv_binding_test.go`, etc.).

**Machine-readable source of truth.** Parsing markdown tables is fragile
(whitespace drift, malformed rows). Instead, the registry document is
paired with a YAML sidecar at
`docs/architecture/invariants.yaml` that contains the same data
structured for machine consumption. The meta-test reads the YAML file
directly; the markdown doc remains the human-readable view.

The YAML sidecar has one entry per invariant:

```yaml
- id: INV-CRYPTO-1
  legacy: [INV-1]
  summary: "Operator MUST NOT see plaintext via live JetStream sub"
  severity: MUST
  status: active
  asserted_by:
    - test/integration/crypto/metadata_only_test.go
  external: false
```

The meta-test:

1. Reads `docs/architecture/invariants.yaml` — no markdown parsing.
2. For each entry where `external` is false, walks the repo's
   `*_test.go` files looking for `// Verifies: <id>` annotations.
3. For entries where `external` is true, verifies the
   `asserted_by` paths exist on disk (frontend tests, lint analyzers).
4. Fails if any entry has zero in-repo bindings.
5. Walks the `docs/superpowers/specs/` tree looking for invariant IDs
   referenced in prose but NOT in the YAML registry — fails if any are
   found (the "no orphan invariants" check).

The YAML file is the drift contract. The markdown doc is the human
presentation. A CI check (part of `task lint`) verifies the two are
consistent — every row in the YAML has a matching row in the markdown
table and vice versa.

The existing per-family meta-tests are retired once the unified test
ships and passes.

### Migration

The cataloging pass covers every invariant currently referenced in
`docs/superpowers/specs/` (62 files), `internal/`, `test/`, and
`plugins/` (407 Go files total). Each invariant is:

1. Added to the registry table under its canonical scope.
2. Renamed in its source spec's invariant section (the prose definition).
3. Renamed in every `// Verifies:` annotation in Go code.
4. Renamed in any test function name that carries the invariant ID.

This is a mechanical, grep-driven pass. No behavior changes.

Annotation density is low (~18 Go files carry `// Verifies:` today);
most code references use bare `INV-N` in comments without the
`Verifies:` prefix. The rename pass is therefore mostly comment-string
substitution, not structural test changes. Plan-stage task sizing MUST
account for the full ~470 file surface.

### Non-goals

- Public-facing curated subset (`site/docs/reference/invariants.md`) —
  deferred to a future, lower-priority bead.
- Re-deriving the naming convention here — it is captured in ADR
  `holomush-6wcf2` (created alongside this spec); this spec references that
  decision rather than restating it, and no further ADR work is in scope.

## Acceptance

- `docs/architecture/invariants.md` exists with a scope index and a
  table per scope.
- Every `I-*`, `INV-*`, and `INV-<PREFIX>-*` identifier currently
  referenced in `docs/superpowers/specs/`, `internal/`, `test/`, and
  `plugins/` has a row in the registry.
- All code and spec references use the canonical `INV-<SCOPE>-<N>` form.
- `test/meta/invariant_registry_test.go` passes; would fail if a new
  invariant is referenced anywhere without a registry entry.
- `task lint` and `task test` are green.
- Per-family meta-tests (`i_priv_coverage_test.go`,
  `inv_binding_test.go`, `i_pres_coverage_test.go`,
  `inv_p4_coverage_meta_test.go`, `inv_p5_coverage_meta_test.go`,
  `scenes_phase6_invariants_test.go`) are retired.
