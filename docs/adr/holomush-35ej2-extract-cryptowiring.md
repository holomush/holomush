<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Extract Manifest-Derived Crypto Helpers to internal/plugin/cryptowiring

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-35ej2
**Deciders:** HoloMUSH Contributors

## Context

Four helpers in `cmd/holomush` (package `main`, therefore unimportable) derive
plugin-crypto/audit wiring from the loaded plugin `Manager`:

- `historyOwnersFromPlugins` (`sub_grpc.go:892-927`) — builds the `audit.OwnerMap`
  that routes plugin-owned subjects, filtering on `PluginAuditClient` registration.
- `buildAlwaysSensitiveSet` (`phase7_fence_wiring.go:66-89`) — the
  `<plugin>:<event_type>` set the PluginDowngradeFence uses (INV-P7-7).
- `newCryptoKeysLookup` (`phase7_fence_wiring.go:101-129`) — the
  `history.CryptoKeysLookup` over the DB pool.
- `buildKeySelector` (`phase7_fence_wiring.go:36`) — the single identity
  `codec.KeySelector` threaded (pointer-identically, INV-P7-9) into the audit
  consumer and the history reader.

The `integrationtest` harness (`internal/testsupport/integrationtest`) needs
these exact derivations to reproduce production crypto routing when completing
the plugin-crypto round-trip (`holomush-5iaov`). Because they live in package
`main`, the harness cannot import them — it must either reimplement them or the
helpers must be extracted to a shared `internal/` package.

A related constraint surfaced during design: there are **two** owner-map
derivations from the same `Manager` — the read-side one above, and an audit-side
one in `core.go:574-590` that additionally gates each owner-entry on `pcm.Add`
success. They are **not** equivalent: the audit-side Add-gate is load-bearing —
marking a subject owned while no consumer runs drops events from every audit sink
(`core.go:561-564`).

## Decision

Extract the four **read-side** manifest-derivation helpers from `cmd/holomush`
into a new shared package `internal/plugin/cryptowiring`, consumed by **both**
`cmd/holomush` (production boot) and the `integrationtest` harness.

The derivation functions take a narrow `ManifestSource` interface
(`ListPlugins`, `AlwaysSensitiveEmitTypes`, `AuditSubjects`, `HasAuditClient`)
rather than `*plugin.Manager` directly; `*plugin.Manager` satisfies it
structurally via a thin `managerSource` adapter at each call site.

The audit-side owner derivation in `core.go` (gated on `pcm.Add` success) is
**intentionally NOT collapsed** into the shared `OwnerMapFromManager`. Only the
read-side derivation is shared; the audit-side stays in place.

## Rationale

- **Single source of truth, compile-time enforced.** Extraction makes divergence
  between the harness and production structurally impossible. Reimplementing the
  derivations in the harness risks the worst kind of test — green in test,
  divergent in prod — with no compile-time guard against drift.
- **The two owner derivations are not equivalent.** Collapsing the read-side and
  audit-side derivations would erase the audit-side `pcm.Add`-success gate, a
  silent correctness regression (owned-but-no-running-consumer ⇒ dropped events).
  So the extraction is deliberately narrowed to the read-side path.
- **Narrow interface for testability.** `ManifestSource` decouples the derivation
  logic from `*plugin.Manager`, so `cryptowiring` unit-tests with fakes (no loaded
  Manager, no testcontainer) and documents the exact read surface the derivations
  require. This is the consequence-level decision that makes the extraction's
  testability goal reachable.
- **Cycle-free placement.** Locating the package under `internal/plugin/` (not
  `internal/eventbus/`) avoids any `eventbus → plugin` import cycle;
  `internal/eventbus/audit` and `internal/eventbus/history` do not import
  `internal/plugin`.
- **Pointer-identity invariant stays enforceable.** Exporting the single
  `KeySelector()` from one package keeps the INV-P7-9 "same instance in audit
  consumer and history reader" invariant testable.

## Alternatives Considered

### Extract to `internal/plugin/cryptowiring` (chosen)

Move the read-side derivations into a shared `internal/` package consumed by both
production boot and the harness. **Strengths:** single source of truth; divergence
between harness and prod is structurally impossible; also makes four previously
unimportable `main`-package helpers independently testable. **Weaknesses:** requires
a production refactor (revises the original "zero prod changes" framing); adds a
package boundary that must stay cycle-free.

### Reimplement the derivations locally in the harness (rejected)

Re-derive owners / always-sensitive / lookup / keyselector inside the harness.
**Strengths:** zero production changes; the harness stays self-contained.
**Weaknesses:** recreates the exact failure mode the harness exists to prevent —
green in test, divergent in prod — with no compile-time guard against drift. Any
future change to the read-side derivation would have to be applied in two places.

### Pass `*plugin.Manager` directly instead of a `ManifestSource` interface (rejected)

Keep the concrete `*plugin.Manager` parameter the original helpers used.
**Strengths:** no adapter; simpler call sites. **Weaknesses:** unit-testing the
derivations would require a fully loaded `Manager` (integration-tier cost), defeating
the extraction's testability goal and coupling the package to the concrete type. The
narrow interface (satisfied structurally by `*plugin.Manager`) was chosen instead.

### Collapse the read-side and audit-side owner derivations into one (rejected)

Unify `historyOwnersFromPlugins` and the `core.go` audit-closure derivation.
**Rejected** because they are not equivalent — the audit side gates each owner-entry
on `pcm.Add` success, a load-bearing difference (owned-but-no-running-consumer drops
events, `core.go:561-564`). Only the read-side derivation is shared; the audit-side
stays in place (D2 narrowed).

## Consequences

**Positive**

- One place to change manifest-to-crypto-routing derivation; updates propagate to
  both production and the harness automatically.
- `cryptowiring` is independently unit-testable with fake `ManifestSource`
  implementations (target ≥90% coverage), with the DB-touching `CryptoKeysLookup`
  query covered by an in-package integration test.
- The `ManifestSource` interface is a stable, documented contract for the
  derivations.

**Negative**

- Requires a production refactor — `cmd/holomush` call sites repointed, the four
  helpers deleted — revising `holomush-5iaov`'s original "zero prod changes" framing.
- A `managerSource` adapter must be maintained (in `cmd/holomush` and a harness
  equivalent) to bridge `*plugin.Manager` to `ManifestSource`.

**Neutral**

- The audit-side owner derivation in `core.go` is intentionally untouched; the
  read-side/audit-side `OwnerMap` asymmetry MUST be documented on the shared
  `OwnerMapFromManager` function.

## References

- Spec: `docs/superpowers/specs/2026-05-27-harness-plugin-crypto-roundtrip-design.md` (§5, §1.2, D2)
- Plan: `docs/superpowers/plans/2026-05-27-harness-plugin-crypto-roundtrip.md` (Tasks 1-4)
- Epic: `holomush-5iaov`
- Related: `holomush-edqh1` (two-gate plugin self-decrypt — read-back authz)
