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
