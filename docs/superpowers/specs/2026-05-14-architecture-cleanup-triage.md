<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Architecture cleanup triage — 2026-05-13 repo audit

- **Date:** 2026-05-14
- **Parent epic:** [`holomush-dj95`](../../../docs/repository-audit/2026-05-13/architecture-review.md) — Repo audit follow-up: architecture
- **Source report:** `docs/repository-audit/2026-05-13/architecture-review.md` (12 prioritized cleanups)
- **Sibling epics:** `holomush-yvdm` (design-alignment), `holomush-1bft` (layering)

## Purpose

This document is the triage output for `holomush-dj95`. It records the decisions taken in the 2026-05-14 brainstorming session that mapped the audit report's 12 prioritized cleanups onto tracked `bd` child beads, recorded inter-finding dependencies, and reconciled overlap with pre-existing beads.

It is **not** an implementation spec. The implementation specs are produced per-bead as the `phase:design` beads enter their own brainstorming passes.

## Method

The brainstorming session walked the audit report's §8 ("Prioritized Architectural Cleanups") and for each cleanup:

1. Searched `bd` for any existing bead covering the same work.
2. Decided: file a new child task, fold into an existing open bead, or defer with rationale.
3. Recorded inter-finding dependencies (e.g., #4 CoreServer decomp blocks on #5 `core.Event` retirement).
4. Assigned a wave (1 = mechanical, 2 = needs design, 3 = blocked / cross-cutting).

## Decisions

### D1 — Granularity: one bead per finding

Each of the 12 audit findings becomes a single tracked child bead of `holomush-dj95`, except where (a) a pre-existing bead already covers the work or (b) the finding splits naturally into a precursor + cleanup pair.

Rationale: enables parallel agent scheduling without bd overhead; the `phase:design` label flags which beads need a brainstorming pass before implementation.

### D2 — Re-parent `ec22.3` rather than file a duplicate

Finding #5 ("Retire `core.Event` + `subjectxlate`") overlaps `holomush-ec22.3` ("Architecture: subjectxlate as permanent shim — decide endgame"). `ec22.3` is re-parented under `dj95` and repurposed as the cleanup bead for finding #5. The `ec22` April 25 audit epic is unaffected by the re-parent (still has its other children).

Rationale: preserves accumulated discussion on `ec22.3`; avoids two beads tracking identical work; single source of truth under `dj95`.

### D3 — Block CoreServer decomp on `core.Event` retirement

The audit notes that `CoreServer`'s shape is partially driven by the `core.Event` vs `eventbus.Event` duality. Decomposing before retiring the duality risks rework in the command-pipeline wing. So finding #4 blocks on `ec22.3` (finding #5).

Rationale: serializes the two largest items but produces a cleaner final shape. The auth-wing and subscribe-history-wing decomposition is independent of the event-type duality, but the project chose serial sequencing over parallel-with-rework.

### D4 — Split finding #1 into precursor + cleanup

Finding #1 ("Eliminate dual history dispatchers") names a specific precursor in the audit text: *"plumb cold-tier auth options through `history.NewReader`"*. This was never filed as a bead. The split:

- `dj95.1` = file + execute the cold-tier-auth precursor.
- `dj95.2` = delete `decodeAuthorizeAndDispatch` and its callsites (depends on `dj95.1`).

Rationale: two PRs, two reviewable units, smaller blast radius per change.

### D5 — Defer finding #2 to a Phase-7 SDK blocker bead

Finding #2 (`WithCryptoEnabled` fossil) requires the Lua and binary plugin SDKs to first plumb `EmitIntent.Sensitive` end-to-end. That work belongs under whatever active Phase-7 epic exists, not under `dj95`. `dj95.3` (the gate-removal bead) is filed as blocked on the SDK-plumbing bead.

Rationale: keeps `dj95` scoped to architecture cleanups; SDK plumbing is feature work that belongs to its domain epic.

### D6 — Audit DLQ already tracked

Finding #11a ("Audit DLQ TODO at `audit/subsystem.go:59`") is already covered by `holomush-1tvn.17`. Cross-link via `bd note` rather than file a duplicate. The AuthGuard audit metrics half (#11b) becomes its own bead (`dj95.11`).

### D7 — Pre-existing overlapping beads: disposition table

A `bd search` against each finding surfaced six additional open beads with material overlap. Per D2's precedent (file new / fold / defer with rationale), each gets an explicit disposition:

| Overlap bead | P | Overlaps | Disposition | Action |
|---|---|---|---|---|
| `holomush-b9uw` | P0 | `dj95.5` (removes one of CoreServer's 24 fields) | **Precursor** — already in flight under `a3a7` epic. Let it finish; `dj95.5` design absorbs the remaining 23 fields. | `bd dep add dj95.5 b9uw` |
| `holomush-a3a7.7` | P0 | `dj95.5` command-pipeline wing | **Precursor** — wiring the unified dispatcher into `CoreServer.HandleCommand` is groundwork for the command-pipeline-wing decomposition. | `bd dep add dj95.5 a3a7.7` |
| `holomush-oirq.10` | P1 | `dj95.5` subscribe-history wing | **Already-merged-shape note** — added `SessionStreamContributor` + `streamRegistry` + `streamCancels` (three of the audit's "24 fields"). `dj95.5`'s subscribe-history-wing design must account for this expanded surface. No dep; just cross-link. | `bd note` on `dj95.5` |
| `holomush-d0tn.20` | P2 | `dj95.8` (rename touches the same type) | **Fold into `dj95.8`** — the close-error-handling fix is one commit inside the rename PR; keeping it separate adds churn since the rename touches every callsite. Close `d0tn.20` with `superseded by dj95.8` once filed. | `bd close d0tn.20` after `dj95.8` files |
| `holomush-0c3r.12` | P1 | duplicate of `holomush-0c3r.3` | **Close as dup** — both filed 2026-03-18 with identical scope. Pick `0c3r.3` as canonical because it has the pending fix-bead `0c3r.23` already wired against it (re-discovered during materialization 2026-05-14). | `bd close 0c3r.12 --reason 'dup of 0c3r.3 (fix-bead 0c3r.23 already attached to 0c3r.3)'` |
| `holomush-0c3r.3` | P1 | `dj95.7` (bug inside `runCoreWithDeps`) | **Independent precursor** — the double-`config.Load` bug is tiny and can land before `dj95.7`'s incremental orchestrator migration. Has open fix-bead `0c3r.23` attached. Don't fold; cross-link only. | `bd note` on `dj95.7` |

Rationale: this is the same logic D2 applied to `ec22.3` — silently filing duplicate beads creates parallel discussion trails and risks double-implementation. The disposition table makes each overlap visible in the audit trail.

## Wave organization

### Wave 1 — mechanical, parallel-safe (no design pass required)

| Bead | Title |
|---|---|
| `dj95.1` | Plumb cold-tier auth options through `history.NewReader` (precursor to `dj95.2`) |
| `dj95.2` | Delete `decodeAuthorizeAndDispatch` + its callsites |
| `dj95.4` | Regenerate `event-interfaces.md` from `bus.go` + add `doc_consistency_test.go` |
| `dj95.8` | Rename `PostgresEventStore` → `SystemInfoStore` + switch `InitGameID` to `idgen.New()` |
| `dj95.10` | Document permitted Lua/binary asymmetries in `.claude/rules/plugin-runtime-symmetry.md` |
| `dj95.11` | Add AuthGuard audit metrics counters (`drain_failed`, `marshal_failed`) |
| `dj95.12` | Lift `focus.Coordinator` from `internal/grpc/focus/` to `internal/focus/` |

### Wave 2 — needs design pass (`phase:design` label)

| Bead | Title |
|---|---|
| `dj95.5` | Decompose `CoreServer` (24 fields) along auth / subscribe-history / command-pipeline wings (blocks on `ec22.3`) |
| `ec22.3` (re-parented) | Retire `core.Event` + `core.EventAppender` + `MemoryEventStore` + `subjectxlate` |
| `dj95.6` | Split `plugin/manager.go` into Loader / Registry / Lifecycle; dedupe `trustAllowlist` |
| `dj95.9` | Reclassify `internal/admin/policy/` under `internal/auditchain/` or `internal/eventbus/crypto/policy/` |

### Wave 3 — blocked or cross-cutting

| Bead | Title |
|---|---|
| `dj95.3` | Resolve `WithCryptoEnabled` fossil — delete gate once SDK `EmitIntent.Sensitive` lands (**blocked on Phase-7-SDK bead**) |
| `dj95.7` | Migrate bootstrap orchestration from `runCoreWithDeps` (1336 LOC) to `lifecycle.Orchestrator` via subsystem `DependsOn` (incremental; may spawn child beads per migrated subsystem) |

**Note on `dj95.7` wave assignment.** Audit §4's direction is mechanically clear (per-subsystem `DependsOn` migration), but the cross-cutting nature — every subsystem touched, may spawn child beads, depends on `dj95.5` for the CoreServer wiring — places it in Wave 3. If you'd rather treat it as `phase:design` Wave 2, the only change is the label; the dep edge stays.

**Note on `dj95.9` design scope.** Audit §1.4 offers two destinations (`internal/auditchain/policy/` or `internal/eventbus/crypto/policy/`). The design pass scope is **destination selection only** — once decided, the move itself is mechanical (directory rename + import updates).

## Bead inventory (13 children of `dj95`)

| Slug | P | Type | Labels | Wave | Audit § |
|---|---|---|---|---|---|
| `dj95.1` | P0 | task | architecture, repo-audit | 1 | 5.1 / 8.P0.1 |
| `dj95.2` | P0 | task | architecture, repo-audit | 1 | 5.1 / 8.P0.1 |
| `dj95.3` | P0 | task | architecture, repo-audit, blocked | 3 | 6.4 / 8.P0.2 |
| `dj95.4` | P0 | task | architecture, repo-audit, docs | 1 | 6.2 / 8.P0.3 |
| `dj95.5` | P1 | task | architecture, repo-audit, phase:design | 2 | 1.2 / 8.P1.4 |
| `ec22.3` *(re-parented)* | P1 | task | architecture, jetstream, meta, review, phase:design | 2 | 1.1 / 8.P1.5 |
| `dj95.6` | P1 | task | architecture, repo-audit, phase:design | 2 | 1.3 / 8.P1.6 |
| `dj95.7` | P1 | task | architecture, repo-audit | 3 | 4 / 8.P1.7 |
| `dj95.8` | P2 | task | architecture, repo-audit | 1 | 2.1 / 6.1 / 8.P2.8 |
| `dj95.9` | P2 | task | architecture, repo-audit, phase:design | 2 | 1.4 / 8.P2.9 |
| `dj95.10` | P2 | task | architecture, repo-audit, docs | 1 | 3 / 8.P2.10 |
| `dj95.11` | P2 | task | architecture, repo-audit | 1 | 5.3 / 8.P2.11 |
| `dj95.12` | P2 | task | architecture, repo-audit | 1 | 7 / 8.P2.12 |

(`Audit §` refers to the section in `docs/repository-audit/2026-05-13/architecture-review.md`.)

## Dependency edges

```bash
bd dep add dj95.2 dj95.1            # delete blocks on cold-tier-auth plumbing
bd dep add dj95.5 ec22.3            # CoreServer decomp blocks on core.Event retirement
bd dep add dj95.5 b9uw              # CoreServer decomp absorbs CoreServer.sessions removal
bd dep add dj95.5 a3a7.7            # CoreServer decomp absorbs unified-dispatcher wiring
bd dep add dj95.7 dj95.5            # bootstrap migration touches NewCoreServer's 20+ options
bd dep add dj95.3 <phase-7-sdk>     # WithCryptoEnabled removal blocks on SDK Sensitive plumbing
```

**Materialization gates** — both must resolve before `bd dep add` is executed:

1. **Phase-7-SDK placeholder.** Locate or file the `EmitIntent.Sensitive` plumbing bead. If absent, file under the active Phase-7 epic — **not** under `dj95`. `dj95.3` carries label `awaiting-precursor` until this resolves; chain materialization skips the edge and notes the gap.
2. **`d0tn.20` close-on-supersede** waits until `dj95.8` is filed (so the `superseded by` link has a valid target).

## Cross-links (notes, not deps)

- `dj95.11` → `bd note "Audit DLQ TODO at audit/subsystem.go:59 is covered by holomush-1tvn.17 (not in this triage)"`.
- `ec22.3` → `bd note "Re-parented under holomush-dj95 (was: holomush-ec22). Repurposed from 'decide endgame' to full retirement of core.Event + EventAppender + MemoryEventStore + subjectxlate per dj95 finding #5."`.

## Out of scope

- Layering and design-alignment findings — tracked under sibling epics `holomush-1bft` and `holomush-yvdm`.
- Humanization findings — tracked elsewhere.
- The Phase-7 SDK `EmitIntent.Sensitive` plumbing itself — feature work, not architecture cleanup.
- Implementation-level design for `phase:design` beads (`dj95.5`, `dj95.6`, `dj95.9`, `ec22.3`) — each gets its own brainstorming session when it comes up for work.

## Acceptance criteria for closing `dj95`

`holomush-dj95` closes when all 13 children close or are explicitly deferred with a `bd note` explaining the rationale.

<!-- adr-capture: optout=true; reason="triage spec — workflow/tactical decisions (bead disposition, scheduling, precursor splits) live in this spec text, not in docs/adr/. Real architectural ADRs surface later in the phase:design child brainstorms (dj95.5 CoreServer decomp, ec22.3 event-type unification, dj95.6 plugin manager split, dj95.9 admin/policy destination)."; session=97760d41; ts=2026-05-15T00:06:32Z -->
