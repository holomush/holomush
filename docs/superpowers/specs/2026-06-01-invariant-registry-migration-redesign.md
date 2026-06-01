<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Invariant Registry Migration Redesign — Safe `INV-<SCOPE>-N` Rename

**Date:** 2026-06-01
**Status:** Draft
**Design bead:** `holomush-hz0v4.14`
**Supersedes:** the rename-mechanism portion (original Tasks 4–7) of
[`2026-05-31-invariant-registry-design.md`](2026-05-31-invariant-registry-design.md)

## Context

The [original invariant-registry design](2026-05-31-invariant-registry-design.md)
established a sound goal — a central registry cataloging every `INV-*`
invariant under a canonical `INV-<SCOPE>-N` naming convention (ADR
`holomush-6wcf2`) — and sound infrastructure: the registry scaffold + YAML
schema (`holomush-hz0v4.1`), a unified drift meta-test with a `binding: pending`
tolerance (`.2`/`.10`), and a YAML↔markdown consistency lint (`.3`). That
infrastructure is preserved (draft PR #4358) and this redesign builds on it.

The original **rename mechanism** (Task 4, `holomush-hz0v4.4`) was unsound and
its execution corrupted the codebase. This spec redesigns the mechanism. The
naming **convention** itself (`INV-<SCOPE>-N`, ADR `holomush-6wcf2`) is
unchanged and remains correct.

### What went wrong (grounded root cause — `holomush-hz0v4.13`)

Before this epic, **many specs independently numbered their invariants from
bare `INV-1`** — crypto `INV-1..52`, phase-8 scene `INV-1..12`, pluginauthz
`INV-1..5`, world `INV-1..2`, commandquery `INV-1`, ABAC-setup `INV-4`,
wholesystem `INV-1/5`, command-introspection `INV-2`, recognized-command-chip
`INV-5`, CI-tooling `INV-6`, and more. They were disambiguated **only by
file/spec context**. This shared bare-`INV-N` namespace is the very mess the
epic exists to unify.

Task 4 was meant to rename **only crypto-spec-origin** refs to `INV-CRYPTO-N`.
Instead it ran a blanket **value-keyed** `sed` (`INV-N → INV-CRYPTO-N` for
`N=1..52` across `internal/`, `test/`, `plugins/`), relabeling **every other
spec's bare `INV-N`** as `INV-CRYPTO-N`. Evidence: `internal/plugin/pluginauthz/`
invariants (runtime parity, subject host-derivation, entitlement, audit) were
stamped `INV-CRYPTO-1..5` — nonsensical as crypto. **Every CI gate passed**
(build, the drift meta-test, the consistency lint) because the corruption lives
in comment/string content that compilers and existence-checking scanners do not
semantically validate.

Two failure modes must both be designed out:

- **F1 — value-keyed rename over a shared namespace.** Keying on the bare
  number `N` cannot distinguish a crypto `INV-3` from a scene `INV-3`.
- **F2 — existence-only verification.** A gate that only checks "this ID is in
  the registry" is blind to *mislabeling*: `INV-CRYPTO-3` stamped on a
  pluginauthz invariant satisfies an existence check.

### Current reference landscape (grounded — probe survey, post-revert)

- **Prefixed families** (~1,150 refs), each prefix globally unique (no
  collision risk; the risk is a wrong family→scope **map**):
  `INV-P7` (189), `INV-RB` (180), `INV-P5` (175), `INV-P4` (118),
  `INV-TS` (107), `INV-GW` (96), `INV-P6` (85), `INV-PC` (29), `INV-FS` (27),
  and the long tail `INV-ROPS`, `INV-RA`, `INV-WS`, `INV-Y5INX`, `INV-W9ML`,
  `INV-M`.
- **Bare `INV-N`** (390 refs) — the shared-namespace problem — concentrated in
  `internal/eventbus/` (45), `internal/plugin/` (23), `test/` (24), and
  `internal/access`, `plugins/core-scenes`, `internal/cluster`,
  `internal/admin`, `internal/world`.

Because multiple families map to one scope (e.g. `P4/P5/P6/FS → SCENE`), the
target `INV-<SCOPE>-N` cannot be a prefix swap — `INV-P4-3` and `INV-P5-3` would
both become `INV-SCENE-3`. The target therefore requires **fresh per-scope
numbering**, not substitution.

## Goals

1. Migrate **all** in-code invariant annotations to canonical `INV-<SCOPE>-N`,
   safely, without corrupting any cross-domain reference.
2. Make recurrence of F1 and F2 **structurally impossible**, not merely
   unlikely.
3. Preserve full old→new traceability.
4. Keep each landing step small and reviewable.

## Non-goals

- Changing the `INV-<SCOPE>-N` naming convention (ADR `holomush-6wcf2` stands).
- Backfilling `// Verifies:` bindings for the ~43 binding-less crypto
  invariants and others — deferred to `holomush-hz0v4.11`; `binding: pending`
  (`.10`) tolerates the gap during migration.
- Re-deriving the registry scaffold, drift meta-test, or consistency lint —
  preserved as-is from `.1`/`.2`/`.3`/`.10` (draft PR #4358).
- A public-facing curated subset (`site/docs/reference/invariants.md`) —
  deferred per the original design.

## Design

### Core principle

> The rename is driven by a **per-ref-classified registry**, never by a
> value-keyed pattern over bare `N`. A reference the registry has not classified
> is never rewritten; and a **deterministic ownership guard** — not an existence
> check — proves each renamed reference sits in a path its scope owns, so a
> mislabel can neither appear nor spread.

### 1. Registry as the authoritative, ref-anchored record

The registry (`docs/architecture/invariants.yaml`, extending the `.1` schema)
MUST record, per invariant:

| Field | Meaning |
| --- | --- |
| `id` | Canonical `INV-<SCOPE>-N` (fresh per-scope numbering). |
| `scope` | The owning scope (e.g. `CRYPTO`, `SCENE`, `PLUGIN`, `EVENTBUS`, `ACCESS`, …). |
| `origin_spec` | Path to the source spec that defines this invariant. |
| `legacy` | List of prior IDs this invariant was known by — bare `INV-N`@`origin_spec`, or `INV-<FAMILY>-N`. Drives traceability and the migration. |
| `summary` | One-line statement of the invariant. |
| `binding` | `asserted_by: <test/spec path>` when known, else `pending` (`.10`). |
| `refs` | The **path-anchored** sites where this invariant is annotated — each a `{file, token}` pair: the source file plus the legacy/canonical ID **token** to anchor on. **Never a line number** — line numbers drift between classification and migration, and a stale line would let the guard pass while pointing at the wrong site. The token is the anchor; the migration tool and guard locate occurrences of that token within `file`. |

The registry also carries a **per-scope record** (in a `scopes:` list) with:

| Scope field | Meaning |
| --- | --- |
| `scope` | Scope name (`CRYPTO`, `SCENE`, …). |
| `owned_paths` | List of path globs this scope owns. Globs **MAY (and for multi-scope directories MUST) target individual files** — e.g. `test/meta/inv_binding_test.go` — not just directories; `test/meta/` alone holds bare `INV-N` from 5+ scopes, so a directory glob there would misassign. `owned_paths` **MUST partition** the annotated tree — no path owned by two scopes — except files on `shared_files`. |
| `shared_files` | List of exact file paths (not globs) that legitimately annotate invariants from more than one scope (e.g. cross-domain integration tests). A file may appear in the `shared_files` of every scope whose migrated tokens it carries; for these files the guard relies on per-ref `refs` site-match alone (the `owned_paths` ownership check is waived, since ownership is intentionally shared). |
| `origin_specs` | The source spec(s) that define this scope's invariants. |
| `status` | `pending` or `migrated`. The provenance guard enforces site-match + ownership only for `migrated` scopes; a `pending` scope's bare `INV-N` is tolerated until its PR lands (mirrors the `binding: pending` tolerance for incremental rollout). |

`legacy` + `refs` + `owned_paths` are load-bearing: together they make the rename a
**closed-world, site-addressed** operation rather than a pattern match, and give
the guard a fully deterministic ownership signal.

### 2. Family→scope map — re-derived from source specs, with evidence

The map MUST be **re-derived from the source specs**, and each family→scope
assignment MUST cite the origin spec that establishes it. Assumed mappings are
forbidden — Task 5's assumed map was ~64% wrong. The map is recorded in this
spec (and, as it is validated per scope, in the registry via each entry's
`origin_spec`).

Starting hypotheses (each MUST be verified against its origin spec before use,
NOT trusted): `P4/P5/P6/FS/FW → SCENE`; `RB → CRYPTO`; `GW → EVENTBUS`;
`PC → PLUGIN`; `P7 → CRYPTO`/`PLUGIN` (a split — the hardest case);
`TS/LOAD/WS/SH/RA → test-infra`/`EVENTBUS`; bare `INV-N` → per-`origin_spec`.
`ROPS`, `Y5INX`, `W9ML`, `M`, `RA` are unclassified and MUST be resolved during
classification.

#### 2.1 Derived family→scope map (holomush-hz0v4.14.2)

Re-derived from each family's defining spec (the spec whose prose *states* the
invariant, not merely a spec that references it). Every row cites a verified
origin-spec path and a one-line evidence note. Where a family splits across
scopes, it appears on multiple rows.

| Family | Scope | Origin spec (path) | Evidence (what in the spec establishes it) |
| --- | --- | --- | --- |
| P4 | INV-SCENE | `docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md` | §12 numbered-invariant table defines INV-P4-1..13: scene event subjects, pose-order gating, IC isolation — all scene-domain behavior. |
| P5 | INV-SCENE | `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md` | §10 defines INV-P5-1..14: focus-membership precondition, focus-key atomicity, terminal-only filter — the scene focus model. |
| P6 | INV-SCENE | `docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md` | §16 defines INV-P6-1..9: publication-vote roster, IsParticipant publish gate, archive-state transitions — scene logs/vote/privacy. |
| FS | INV-SCENE | `docs/superpowers/specs/2026-05-28-focus-delta-coordinator-unification-design.md` | Table defines INV-FS-1..7: per-connection focus-delta delivery driven inside `focus.Coordinator` — the scene focus-delivery model (subsumes ex-ymgjs INV-FW-*). |
| FW | INV-SCENE | `docs/superpowers/specs/2026-05-28-focus-delta-coordinator-unification-design.md` | INV-FW-1/2/4/5 are explicitly re-stated as INV-FS-2/4/5/6 ("ex-ymgjs INV-FW-N"); same focus-delivery domain. |
| Y5INX | INV-SCENE | `docs/superpowers/specs/2026-05-28-scene-bare-ulid-identity-design.md` | INV-Y5INX-1..4: `newSceneID()` bare ULID, scene readable via real RPC, join opens focus subscription, history-scope floor — scene identity/lifecycle. |
| RB | INV-CRYPTO | `docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md` | INV-RB-1..10: host-side read-back decryption, DEK never reaches plugin, AAD round-trip, `plugin_decrypt` audit, downgrade/DEK-existence reuse — cryptographic operations on payloads. (RB-9 runtime symmetry is a property *of* the crypto primitive, not a plugin-system invariant.) |
| P7 | INV-CRYPTO | `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` | SPLIT — crypto half: INV-P7-1,2,5,6,7,9,11,12,15,16 = ciphertext byte-equality, DEK-existence fence, downgrade refusal, AAD reconstruction, shared KeySelector. These are payload-encryption invariants (carry master INV-25/26/46/48/50 onto the plugin-routed path). |
| P7 | INV-EVENTBUS | `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` | SPLIT — eventbus/audit-plumbing half: INV-P7-3,4,10 = plugin audit-table DEK columns + header parser + standalone migration ordering. These are audit-projection/dispatcher plumbing (host-owned-vs-plugin-owned audit), the INV-EVENTBUS "audit projection" surface. (INV-P7-13/14 plugin-role/sensitivity-gate are corroborating boundary checks that ride with the crypto half.) |
| GW | INV-EVENTBUS | `docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md` | INV-GW-1..16: verb-registry sourcing, `EMIT_UNKNOWN_VERB`/`EMIT_VALIDATION_FAILED` paths, rendering-field propagation, `events_audit.rendering` column, enum parity — event rendering-completeness, an INV-EVENTBUS surface. |
| ROPS | INV-EVENTBUS | `docs/superpowers/specs/2026-05-29-colon-style-subject-eradication-design.md` | INV-ROPS-1..3: colon-style subjects survive only as ABAC policy-DSL identifiers; `QueryStreamHistory`/`Subscribe` qualify to dot-form; repo-wide scan fails CI on surviving colon streams — subject-naming/colon-eradication, INV-EVENTBUS. |
| PC | INV-PLUGIN | `docs/superpowers/specs/2026-05-26-plugin-runtime-config-design.md` | INV-PC-1,8: host MUST NOT interpret plugin config-key meaning; `needsInit` gate includes `len(Config)>0` — plugin-system manifest/runtime contract. |
| W9ML | INV-PLUGIN | `docs/superpowers/specs/2026-05-04-legacy-id-elimination-design.md` | INV-W9ML-1..8: uniform ULID identity per actor kind, `IdentityRegistry` resolution path, plugin-name uniqueness/stable-ULID across retention — plugin identity contract. (Spans store/eventbus annotation sites, but the invariant *is* plugin identity.) |
| RA | INV-ACCESS | `docs/superpowers/specs/2026-05-26-harness-real-abac-design.md` | INV-RA-1..4: `WithRealABAC()` wires the real access engine and installs the `seed:*` policy set; allow-all retained without it — ABAC policy-evaluation harness wiring. |
| TS | INV-STORE | `docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md` | INV-TS-1..7: all persistent time = `BIGINT` epoch-ns, no new `TIMESTAMPTZ` migrations, `pgnanos.Time` canonical scan/insert seam, no `Truncate(µs)` — migration discipline + storage seam. (Annotated widely because every nanos round-trip touches it; the invariant is database/storage.) |
| M | INV-SESSION | `docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md` | INV-M-1..4: `session.Store` has exactly one production impl (`PostgresSessionStore`), `sessiontest.NewStore` isolation, `client_type` rejection semantics — session-store state machine. |
| WS | (test-infra) → INV-PLUGIN | `docs/superpowers/specs/2026-05-25-wholesystem-plugin-integration-design.md` | INV-WS-1..4: `WithInTreePlugins()` reuses `setup.PluginSubsystem`, asserts cross-plugin ABAC permit+forbid, not silently skipped, opt-in — whole-system plugin-load harness; owned under INV-PLUGIN (plugin-subsystem load discipline). |
| LOAD | (test-infra) → INV-TELEMETRY | `docs/superpowers/specs/2026-05-28-load-perf-testing-harness-design.md` | INV-LOAD-1..4: harness drives web/telnet tiers, latency thresholds, k6-exit-code verdict — load/perf observability harness; owned under INV-TELEMETRY (latency/metric verdicts). |
| SH | (test-infra) → INV-SCENE | `docs/superpowers/specs/2026-05-27-shcyu-harness-publish-driving-design.md` | INV-SH-1..5: plugin-config overrides reach core-scenes, `SceneServiceClient` resolves the scene plugin, `CreateScene` returns a real ULID, zero production code, publish lifecycle E2E — scene publish-driving harness; owned under INV-SCENE. |
| bare `INV-N` | per-origin-spec | `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (master) + `docs/architecture/invariants.md`-tracked | NOT a single scope. INV-1..27/30..55 (crypto payload/DEK/KEK/AAD) → INV-CRYPTO (`internal/eventbus/crypto/**`, `internal/eventbus/history/**`). INV-28/29/56/59 (N-of-N invalidation ack, Coordinator retry, cache-invalidation correctness) → INV-CLUSTER (`internal/cluster/**`) per the existing INV-CLUSTER boundary note. Each bare token is scoped by *its* origin spec, not forced into one family. |

### 3. Per-ref classification

For each scope, classification proceeds per reference using three signals, in
order: (a) the **origin spec** that defines the invariant; (b) the **annotation
text** at the ref site, matched against the origin spec's invariant statement;
(c) the **file/package path** as a corroborating signal. The output is registry
entries populated with `legacy` + `refs`. Classification output for a scope MUST
be human-reviewed before its migration lands.

### 4. Migration tool — closed-world, site-addressed, idempotent

The migration tool MUST rewrite **only the `{file, token}` sites recorded in the
registry's `refs`** (the file path plus the legacy ID token to anchor on — never
a line number) for the invariants of the scope being migrated, mapping each
`legacy` token to its canonical `id` within that file. It MUST NOT match on bare
`INV-N` values across the tree. It MUST be idempotent (re-running yields no
change once applied) and MUST operate on one scope at a time. A `file` not
present in any registry entry's `refs` is, by construction, never touched. Where
one file carries bare `INV-N` tokens belonging to more than one scope, each
distinct (legacy token → canonical id) mapping is recorded separately in `refs`,
so the rewrite stays unambiguous per token.

### 5. Provenance guard — fully deterministic, defeats F2

The drift meta-test (`.2`) is extended into a **provenance guard**. It runs in
CI and MUST be **fully deterministic** — no LLM call, no fuzzy threshold, no
keyword heuristic at gate time. Classification (which *may* be LLM-assisted)
happens once, up front; its reviewed product is the registry, and the **guard
only verifies the registry's internal consistency against the live tree**. For
every `INV-<SCOPE>-N` reference found in code, in a scope whose `status` is
`migrated`:

1. **Existence** — the canonical ID is present in the registry.
2. **Site match** — the ref's `file` (path-anchored, not line) is listed in that
   entry's `refs`. The migration tool only writes tokens at recorded sites, so a
   canonical token appearing at an unrecorded path fails — a mislabel cannot
   propagate.
3. **Ownership** — the ref's `file` is in its scope's `owned_paths` (or on that
   scope's `shared_files` allowlist). A scene-file annotation stamped
   `INV-CRYPTO-*` fails, because the scene file is not in `CRYPTO.owned_paths`
   and `owned_paths` partition. This is the deterministic F2 defense: it needs
   no text comparison.

**What the guard does and does not prove.** Site-match + ownership make a
mislabel *structurally unable to appear or spread* in a migrated scope. They do
**not** independently re-prove that classification put `INV-3` in the *right*
scope to begin with — that correctness is established by the **reviewed per-scope
diff** (feasible precisely because each scope lands as one small PR), and is what
the `owned_paths` partition corroborates. `summary` remains a human-review aid,
not a gate input.

Any residual **bare `INV-N`** in a `migrated` scope MUST fail the guard (loud,
not silent); bare `INV-N` in a `pending` scope is tolerated until that scope's PR
lands. The existing consistency lint (`.3`) continues to enforce YAML↔markdown
parity; `binding: pending` (`.10`) tolerates the binding gap. A **negative test**
MUST seed a deliberately mislabeled ref and assert the guard fails on it.

### Data flow

```text
origin specs
   → per-ref classification (origin spec ▸ annotation text ▸ path)
   → registry entries (scope, legacy, refs, binding, summary)
   → migration tool rewrites recorded refs (legacy → canonical id)
   → provenance guard + consistency lint verify
   → land (one scope)
```

### Execution — per-scope incremental (replaces Tasks 4–9)

1. **Re-derive + evidence the family→scope map** (one task; output reviewed).
2. **One bead/PR per scope**: classify → populate registry → migrate that
   scope's recorded refs → guard + lint green → land. Ordered **easiest scope
   first** to validate the mechanism end-to-end on a small, clean scope; the
   **`P7` CRYPTO/PLUGIN split is sequenced last** as the hardest case.
3. **Retire the per-family meta-tests** (`holomush-hz0v4.8`) only **after** all
   scopes are migrated and the provenance guard subsumes them.
4. **Final verification** (`holomush-hz0v4.9`): no bare `INV-N` remains, guard +
   lint green, registry complete.

Binding backfill (`holomush-hz0v4.11`) remains a follow-up.

## Error handling & safety

- **Closed-world rename (defeats F1):** only registry-recorded `refs` are
  rewritten; an unclassified reference is left bare and trips the guard — silent
  cross-domain corruption is structurally impossible.
- **Ownership guard (defeats F2):** site-match + `owned_paths` ownership catch
  the exact mislabel Task 4's green CI missed (a pluginauthz file stamped
  `INV-CRYPTO-*` is not in `CRYPTO.owned_paths`), with no fuzzy text matching.
- **Bounded blast radius:** one scope per PR; the family map is validated
  progressively rather than all-or-nothing.
- **Traceability:** `legacy` aliases preserve old→new history and let the guard
  reconcile references during the transition window.

## Testing

- **Provenance guard** (extends `.2`): existence + site-match + `owned_paths`
  ownership; a "no un-migrated bare `INV-N` in a `migrated` scope" assertion; and
  a **negative test** seeding a deliberately mislabeled ref and asserting the
  guard fails on it.
- **`owned_paths` partition test:** no path is owned by two scopes (except listed
  `shared_files`).
- **Consistency lint** (`.3`): YAML↔markdown parity.
- **`binding: pending` tolerance** (`.10`): the guard does not require a
  `binding` for `pending` entries.
- **Migration-tool tests:** idempotence; refuses any site not in `refs`; renames
  only the target scope.
- **Per-scope acceptance:** every migrated ref resolves to a registry `id`, and
  guard + lint are green before the scope's PR lands.

## Acceptance

- The family→scope map is re-derived with per-family origin-spec evidence.
- Every in-code invariant annotation is migrated to `INV-<SCOPE>-N` via the
  closed-world, site-addressed tool — no value-keyed rename anywhere.
- The provenance guard enforces existence + site-match + `owned_paths`
  ownership, is deterministic (no LLM/fuzzy match at gate time), and is green; it
  demonstrably fails on a seeded mislabel (a negative test).
- `owned_paths` partition the annotated tree (no path in two scopes, except
  listed `shared_files`).
- No bare `INV-N` remains in any `migrated` scope.
- `task lint` and `task test` are green; per-family meta-tests retired only
  after the guard subsumes them.
<!-- adr-capture: sha256=54306f703f33c26d; session=cli; ts=2026-06-01T15:03:24Z; adrs= -->
