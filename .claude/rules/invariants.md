<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "docs/architecture/invariants.yaml"
  - "docs/architecture/invariants.md"
  - "test/meta/invariant_registry_test.go"
  - "cmd/inv-render/**"
  - "cmd/inv-migrate/**"
  - "internal/invregistry/**"
  - "docs/specs/**/*.md"
  - "docs/superpowers/specs/**/*.md"
---

# Invariant Registry

HoloMUSH has **one** canonical registry of named system invariants. This rule
auto-loads when you touch the registry, its tooling, or any spec — because a
spec is where invariants are *born*, and the registry is where they must land.

| Artifact | Role |
| --- | --- |
| `docs/architecture/invariants.yaml` | **Source of truth.** Every invariant: `id` (`INV-<SCOPE>-N`), `scope`, `origin_spec`, `legacy`, `summary`, `binding`, optional `asserted_by`/`refs`. |
| `docs/architecture/invariants.md` | **Generated** from the YAML by `go run ./cmd/inv-render`. NEVER hand-edit inside the `<!-- BEGIN GENERATED: <region> -->` … `<!-- END GENERATED: <region> -->` regions (`scope-index`, `invariant-tables`). |
| `test/meta/invariant_registry_test.go` | CI guard: drift (generate-and-diff), provenance/ownership, binding presence, spec-orphan detection. |
| `internal/invregistry/` | The single Go schema definition shared by the tooling. |

The full design is `docs/superpowers/specs/2026-05-31-invariant-registry-design.md`.
The live binding-backfill status lives in `docs/roadmap.md` → Maintenance.

## What rises to a registry invariant

Register a guarantee when it is a **durable property that future work could
silently break and that a test can pin forever**. Rule of thumb: if violating it
is a *regression in a guarantee* rather than a *missing feature*, it is an
invariant. Do NOT register every RFC2119 MUST: a local feature requirement ("the
create-scene RPC MUST return the new ID") is not an invariant.

### In scope vs. out of scope

| In scope | Out of scope |
| --- | --- |
| **System-behavior guarantees** — fail-closed ABAC defaults, no-plaintext-to-non-participant, runtime symmetry, ordering ownership. The core of the registry. | **Migration / refactor-completeness bookkeeping** — "every pre-consolidation test is named in a `// replaces:` chain". Tracks a one-time migration, not a durable guarantee. |
| **Test-infrastructure fidelity** — durable guarantees about *how the system is verified* (e.g. "the load harness drives the real Connect/TCP path, never a stub"; "`sessiontest.NewStore` returns an isolated store per call"). These can be silently broken and are worth pinning. Keep them, but make the summary state the **guarantee**, not the harness wiring. | **Self-admitted documentary / non-testable notes** — anything whose own summary says "documentary", "verified by spec review", "operational property, not an invariant", or describes a transient phase-implementation detail ("Phase 4 introduces no status transitions"). If no test can fail when it's violated, it is not an invariant. |

Borderline test-infra entries SHOULD be phrased as the property under test, not
the test's construction ("no `t.Fatalf`", "adds zero production code", "E6
acceptance" are PR acceptance criteria, not invariants).

Pick the **scope** by its declared `boundary` in `invariants.yaml` (CRYPTO,
SCENE, PLUGIN, EVENTBUS, CLUSTER, ACCESS, SESSION, STORE, TELEMETRY, PRIVACY,
PRESENCE, COMMAND — all migrated; BRANDING/DOCS pending). Allocate the next free
`N` in that scope.

## Design-time: defining and respecting invariants

When **authoring or reviewing a spec**:

| Requirement | Rule |
| --- | --- |
| **MUST** capture, not scatter | When a spec introduces a system-level guarantee, give it a canonical `INV-<SCOPE>-N` id and add it to `invariants.yaml` as part of finalizing the spec. Do NOT invent a fresh ad-hoc family (`I-FOO-1`, `INV-XY-3`). The whole registry exists because pre-2026-05 specs each minted their own un-indexed family and a migration (epic `holomush-hz0v4`) had to dig them all out and renumber. Don't recreate that debt. |
| **MUST** consult before designing | Before designing in a domain, read the registry entries for the relevant scope(s). Your design MUST NOT silently violate, duplicate, or contradict an existing invariant. |
| **MUST** update on change | If a design changes or retires an existing invariant, edit its registry entry (and `origin_spec`) in the same change — never leave the registry describing a guarantee the code no longer makes. |
| **MUST NOT** renumber casually | Canonical ids are referenced from tests and other specs. Renaming is a migration, not an edit. Use the `legacy:` list to preserve the old token's provenance. |

A new invariant ships `binding: pending` (no test yet) or `binding: bound`
(test exists; see below) — never as bare prose only. The meta-test's
orphan check fails CI if a spec **under `docs/superpowers/specs/`** references an
`INV-<migrated-scope>-N` that is not in the registry. Note the limit: the check
walks only `docs/superpowers/specs/` — invariants introduced in a `docs/specs/`
spec (or in code; see below) are NOT auto-caught and MUST be registered by hand.

## Implementation-time: the binding ratchet

An invariant is only *proven* when a test asserts it. The mechanism:

1. Write/locate the `*_test.go` that genuinely asserts the invariant (not one
   that merely touches the code).
2. Annotate it: `// Verifies: INV-<SCOPE>-N` (one line, immediately above the
   asserting test or the assertion block).
3. In `invariants.yaml`, set the entry `binding: bound` and add `asserted_by:`
   listing the verifying test file(s).
4. Regenerate: `go run ./cmd/inv-render`.
5. Confirm: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`.

| Requirement | Rule |
| --- | --- |
| **MUST NOT** fabricate a binding | If no test genuinely asserts the invariant, that is a real coverage gap — file `bd create -t bug` and leave it `binding: pending`. A `// Verifies:` on a test that doesn't actually prove the invariant is a false-green (this is precisely what bug `holomush-0sh1k`/INV-RB-3 was, and INV-PRIVACY-7 bound to a `Skip()` placeholder). |
| **MUST NOT** bind a `Skip`/placeholder test | `TestBoundInvariantsAreGenuinelyAsserted` fails CI if a `bound` entry's every `// Verifies:` site is a Skip-only placeholder with no assertion. Note its limit: it cannot detect a *partial* binding (a test that asserts only one clause of a multi-clause invariant — that needs human review, as INV-PRIVACY-6 did). |
| **MUST NOT** carry `asserted_by` while pending | The meta-test rejects a `binding: pending` entry that lists `asserted_by` — no fabricated provenance. |
| **MAY** stay pending | `pending` is a tolerated state (decision `holomush-hz0v4.10`); backfill is a ratchet, not a blocker. Per-scope backfill is tracked under epic `holomush-hz0v4`. |

### Known escape hatches — register these by hand

The orphan check walks only `docs/superpowers/specs/`. It does **not** scan code,
and it does **not** scan `docs/specs/`. So a new canonical-form `INV-<SCOPE>-N`
introduced only in code/tests, or only in a `docs/specs/` spec, is NOT auto-caught
and will silently be missing from the registry. In either case you MUST add the
registry entry yourself — the meta-test will not remind you.
