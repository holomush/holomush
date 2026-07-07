<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Invariant registry — deep reference

## In scope vs. out of scope

| In scope | Out of scope |
| --- | --- |
| **System-behavior guarantees** — fail-closed ABAC defaults, no-plaintext-to-non-participant, runtime symmetry, ordering ownership. The core of the registry. | **Migration / refactor-completeness bookkeeping** — "every pre-consolidation test is named in a `// replaces:` chain". Tracks a one-time migration, not a durable guarantee. |
| **Test-infrastructure fidelity** — durable guarantees about *how the system is verified* (e.g. "the load harness drives the real Connect/TCP path, never a stub"; "`sessiontest.NewStore` returns an isolated store per call"). These can be silently broken and are worth pinning. Keep them, but make the summary state the **guarantee**, not the harness wiring. | **Self-admitted documentary / non-testable notes** — anything whose own summary says "documentary", "verified by spec review", "operational property, not an invariant", or describes a transient phase-implementation detail ("Phase 4 introduces no status transitions"). If no test can fail when it's violated, it is not an invariant. |

Borderline test-infra entries SHOULD be phrased as the property under test, not
the test's construction ("no `t.Fatalf`", "adds zero production code", "E6
acceptance" are PR acceptance criteria, not invariants).

## Why the registry exists

The whole registry exists because pre-2026-05 specs each minted their own
un-indexed family and a migration (epic `holomush-hz0v4`) had to dig them all
out and renumber. Don't recreate that debt.

## Known escape hatches — register these by hand

The orphan check walks only `docs/superpowers/specs/`. It does **not** scan code,
and it does **not** scan `docs/specs/`. So a new canonical-form `INV-<SCOPE>-N`
introduced only in code/tests, or only in a `docs/specs/` spec, is NOT auto-caught
and will silently be missing from the registry — the meta-test will not remind you.
