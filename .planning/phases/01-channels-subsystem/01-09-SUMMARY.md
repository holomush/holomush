---
phase: 01-channels-subsystem
plan: 09
subsystem: core-channels
tags: [core-channels, whole-system-census, e2e, invariant-registry, session-stream-delivery, phase-gate]

# Dependency graph
requires:
  - phase: 01-08
    provides: QuerySessionStreams (memberships ∪ defaults) + mid-session join/leave live delivery (stream.subscription)
  - phase: 01-06
    provides: membership-gated QueryChannelHistory + emit→audit projection (channel_log)
  - phase: 01-05b
    provides: full ChannelService RPC surface (Create/Join/Leave/List/Post/Who/QueryHistory/Invite/…)
  - phase: 01-03
    provides: history_scope custom manifest + seeded default channels
provides:
  - whole-system census asserting core-channels loads (INV-PLUGIN-54 fail-closed proof)
  - cross-cutting channels e2e proving membership-gated live delivery + history + uniform not-found + admin override + MED-6 = routing
  - registered INV-CHANNEL-1/2 and bound INV-PRIVACY-7 (first history_scope custom adopter)
  - integrationtest WithSessionStreamDelivery() harness option (plugin session-stream + audit delivery, gated)
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A gated harness option (WithSessionStreamDelivery) threads ONE SessionStreamRegistry into both the plugin subsystem (stream.subscription AddSessionStream target) and the CoreServer (WithStreamRegistry + WithStreamContributor), mirroring cmd/holomush/core.go — zero blast radius to existing suites"
    - "User-facing plugin command outcomes (Errorf/OK) surface as command_error / command_response EVENTS, not RPC failures — asserted via WaitForEvent + CommandResponsePayload, never SendCommand's return"
    - "A create-time ABAC gate must resolve its sentinel resource (channel:new) to an EMPTY attribute bag, not a fail-closed NotFound — the real seeded engine DOES invoke the resolver (the 'resolver is never called' assumption holds only for a mock)"

key-files:
  created:
    - test/integration/channels/channels_suite_test.go
    - test/integration/channels/channels_e2e_test.go
  modified:
    - test/integration/wholesystem/census_test.go
    - internal/testsupport/integrationtest/harness.go
    - internal/testsupport/integrationtest/plugins.go
    - plugins/core-channels/resolver.go
    - plugins/core-channels/service.go
    - plugins/core-channels/resolver_test.go
    - test/integration/privacy/privacy_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "INV-CHANNEL-1 summary explicitly scopes the 01-04 public-read permit to VISIBILITY / join-eligibility (NOT history content), reconciling review LOW/MED-7: history CONTENT is membership-gated for ALL channel types by the 01-06 QueryHistory fence, so a public channel's openness does not exempt its history"
  - "INV-S7 (N=2 second-substrate-consumer) is a spec-only/process invariant with NO registry entry (per invariants.yaml INV-SCENE comment); CHAN-05 is validated STRUCTURALLY — the whole-system census loading BOTH core-channels and core-scenes is the N=2 proof"
  - "INV-PRIVACY-7 flipped from pending (Skip placeholder) to bound: core-channels is the FIRST history_scope custom adopter and the channels e2e genuinely exercises its divergent membership-gated custom-scope QueryChannelHistory; the superseded Skip placeholder in privacy_test.go was removed"
  - "WithSessionStreamDelivery bundles the full plaintext plugin-delivery substrate (stream registry + plain rendering emitter + audit-projection consumer + PluginSubsystemConfig.GameID) so channel live delivery AND emit→audit round-trip work end-to-end without crypto"

requirements-completed: [CHAN-01, CHAN-02, CHAN-03, CHAN-04, CHAN-05]

coverage:
  - id: E1
    description: "core-channels loads under the whole-system census (INV-PLUGIN-54 fail-closed; emits:[channel]+history_scope:custom validates in ParseManifest)"
    requirement: "CHAN-05"
    verification:
      - kind: integration
        ref: "test/integration/wholesystem/census_test.go (expectedPlugins)"
        status: pass
    human_judgment: false
  - id: E2
    description: "A joined member receives another member's channel_say live; a non-member receives nothing (T-01-01)"
    requirement: "CHAN-02"
    verification:
      - kind: integration
        ref: "test/integration/channels/channels_e2e_test.go (live delivery + non-member no-delivery It)"
        status: pass
    human_judgment: false
  - id: E3
    description: "A member reads posted content back via membership-gated QueryChannelHistory (emit→audit round-trip); a non-member is denied a uniform NotFound (INV-CHANNEL-1 / CHAN-03)"
    requirement: "CHAN-03"
    verification:
      - kind: integration
        ref: "test/integration/channels/channels_e2e_test.go#reads-posted-content-back (// Verifies: INV-CHANNEL-1, INV-PRIVACY-7)"
        status: pass
    human_judgment: false
  - id: E4
    description: "A hidden (private, non-invitee) channel and an absent channel present the IDENTICAL uniform not-found (INV-CHANNEL-2 / T-01-12); a non-owner admin overrides"
    requirement: "CHAN-04"
    verification:
      - kind: integration
        ref: "test/integration/channels/channels_e2e_test.go#uniform-not-found (// Verifies: INV-CHANNEL-2) + admin-override It"
        status: pass
    human_judgment: false
  - id: E5
    description: "A raw-input `=Townsquare ...` routes through the manifest-seeded `=` prefix alias to core-channels and posts (MED-6)"
    requirement: "CHAN-01"
    verification:
      - kind: integration
        ref: "test/integration/channels/channels_e2e_test.go#=alias-routing It"
        status: pass
    human_judgment: false

# Metrics
duration: 150min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 09: Whole-subsystem proof + invariant registration Summary

**Proves the channels subsystem end-to-end (whole-system census + cross-cutting Ginkgo e2e) and registers its guarantees: a joined member receives another member's `channel_say` live while a non-member receives nothing and is denied history content; a hidden channel is indistinguishable from an absent one with an admin override; a posted line round-trips emit→audit→read; `=Public hello` routes via the seeded `=` alias — closing CHAN-01..05, binding INV-CHANNEL-1/2 + INV-PRIVACY-7, and validating INV-S7 (N=2) structurally.**

## Performance

- **Duration:** ~150 min (including debugging two real integration bugs the e2e surfaced)
- **Completed:** 2026-07-08
- **Tasks:** 3 (Task 2 TDD)
- **Files:** 2 created, 9 modified

## Accomplishments

- **Task 1 — whole-system census.** Added `core-channels` to `expectedPlugins` in `test/integration/wholesystem/census_test.go`. The load through the real `Manager.LoadAll` path is the end-to-end proof of INV-PLUGIN-54 (its `eval` + `stream.subscription` capability declarations satisfy fail-closed load) AND that the 01-03 manifest validates — `emits: [channel]` + `history_scope: custom` passes `ParseManifest` (a `history_scope: channel` value would fail the closed-enum validator and fail this census).
- **Task 2 — cross-cutting e2e.** `test/integration/channels/channels_e2e_test.go` (Ginkgo, `//go:build integration`, real in-process stack via `WithInTreePlugins + WithRealABAC + WithSessionStreamDelivery`) proves: (a) a second joined member receives `channel_say` live while a non-member does not (T-01-01 both arms); (b) a member reads posted content back through the membership-gated `QueryChannelHistory` (emit→audit→read round-trip, CHAN-03) while a non-member is denied a uniform `NotFound` (INV-CHANNEL-1); (c) a hidden private channel and an absent channel present the SAME uniform not-found (INV-CHANNEL-2) and a non-owner admin overrides (D-06); (d) `=Townsquare ...` routes through the manifest-seeded `=` prefix alias to core-channels and posts (MED-6 live routing hop, not just the 01-07 parser unit test).
- **Task 3 — invariant registration.** Registered the `INV-CHANNEL` scope + `INV-CHANNEL-1` (membership-gated history content for every channel type) and `INV-CHANNEL-2` (channel error uniformity), `binding: bound` via genuine `// Verifies:` annotations in the e2e. Flipped `INV-PRIVACY-7` to bound (core-channels is the first `history_scope: custom` adopter) and removed the superseded Skip placeholder. Regenerated `invariants.md` in the same change. Meta-tests (`TestEveryRegistryInvariantHasBinding` / `TestBoundInvariantsAreGenuinelyAsserted` / `TestProvenanceGuard` / `TestOwnedPathsPartition`) all green.

## CHAN-05 / INV-S7 (N=2 second-substrate-consumer)

INV-S7 is a **spec-only / process invariant** with no registry entry (per the `INV-SCENE` scope comment in `invariants.yaml`: "S1/S2/S6/S7/S8/S10 are spec-only/process invariants — no code refs"). CHAN-05 is therefore validated **structurally**: the whole-system census now loads BOTH `core-channels` and `core-scenes`, each consuming the identical substrate seams (store / audit projection / attribute resolver / emit fence). That two-consumer load IS the N=2 proof; no INV-S7 registry entry is minted (left to the substrate-owner's judgment, per plan).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Channel creation was broken under real ABAC**

- **Found during:** Task 2 (the e2e was the first exercise of `channel create` under the real seeded engine).
- **Issue:** The create gate evaluates ABAC against the sentinel `channel:new` (`createRateResource`) before any channel row exists. The manifest/service comment asserted "the resolver is never called" for this ref — true only for a **mock** evaluator. The real seeded engine DOES invoke `ChannelResolver.ResolveResource`, which returned a fail-closed `CHANNEL_NOT_FOUND` for the sentinel id, denying **every** create (channels were unusable in production under real ABAC).
- **Fix:** `ResolveResource` now special-cases the create sentinel id (`createSentinelResourceID = "new"`) to return an EMPTY attribute bag — the principal-only admin-create policy fires; genuine missing-channel reads still return the uniform NotFound. Added `TestChannelResolverCreateSentinelResolvesToEmptyAttributes`.
- **Files modified:** `plugins/core-channels/resolver.go`, `plugins/core-channels/service.go`, `plugins/core-channels/resolver_test.go`.
- **Commit:** `5b434656c`

### Auto-added blocking infrastructure (harness)

**2. [Rule 3 - Blocking] `WithSessionStreamDelivery()` harness option**

The e2e cannot prove live delivery / emit→audit without the plugin session-stream + audit substrate wired in the harness (previously only `WithFocusDelivery`/`WithPluginCrypto` wired fragments). Added a gated `WithSessionStreamDelivery()` that mirrors production (`cmd/holomush/core.go`): threads ONE `SessionStreamRegistry` into both the plugin subsystem and the CoreServer (`WithStreamRegistry` + `WithStreamContributor`), wires a plaintext rendering publisher into the plugin event emitter and the plugin audit-projection consumer, and — the key fix — sets `PluginSubsystemConfig.GameID` so the plugin `stream.subscription` capability can qualify relative `channel.<id>` refs (a missing GameID caused `STREAM_QUALIFY_FAILED` and degraded live delivery). Gated → zero blast radius; the full `task test:int` (10394 tests) confirms no regression to other plugin suites. Added a `ChannelServiceClient()` resolver. [Files: `internal/testsupport/integrationtest/harness.go`, `plugins.go`]

### Test-shape correction (no product change)

**3. Command outcomes are events, not RPC returns.** A plugin `Errorf`/`OK` response is delivered to the acting session as a `command_error` / `command_response` EVENT — `SendCommand` returns nil (dispatch succeeded). The uniformity/override assertions read the delivered `CommandResponsePayload.Text` via `WaitForEvent`, mirroring the scenes e2e (`scene_info_read_access_test.go`).

## Verification

- `task test:int` — **10394 tests, 5 skipped**, exit 0 (census + channels e2e + privacy + meta + all other suites; no regression from the unconditional `GameID` / gated delivery wiring).
- `task test` — 10090 unit tests, exit 0.
- `task lint:go` — 0 issues. `task lint:invariants` (inv-render -check) — clean. `task lint` markdown reports 19 issues ALL in pre-existing `.planning/` GSD docs (out of scope, tracked in `deferred-items.md` since 01-01); zero in authored source/docs.
- Invariant meta-tests — 29 pass (`TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted|TestProvenanceGuard|TestOwnedPathsPartition|TestRegistry*`).

## Threat mitigations (this plan)

| Threat | Mitigation | Status |
| --- | --- | --- |
| T-01-01 (non-member read/live-delivery) | e2e asserts non-member QueryChannelHistory NotFound AND non-member receives no channel_say; binds INV-CHANNEL-1 | mitigated |
| T-01-12 (hidden-channel oracle) | e2e asserts private non-invitee join == absent join (identical uniform not-found); binds INV-CHANNEL-2 | mitigated |
| T-01-21 (plugin loads with undeclared capability) | whole-system census exercises INV-PLUGIN-54 fail-closed load | mitigated |

## Prohibition status (T-01-01, was pending from 01-08)

**Discharged.** The channels e2e binds it in both arms: a non-member's `QueryChannelHistory` is denied a uniform `NotFound` (history CONTENT membership-gated for public too) AND a non-member receives no `channel_say` on its live stream; the private non-invitee join returns the uniform not-found.

## Task Commits

1. **Task 1: census entry** — `8ff6d79a6` (test)
2. **Deviation Rule 1: create-sentinel resolver fix** — `5b434656c` (fix)
3. **Task 2: channels e2e + session-stream delivery harness** — `1b41d6dcf` (test)
4. **Task 3: register INV-CHANNEL-1/2, bind INV-PRIVACY-7** — `35b6c61dc` (docs)

## Self-Check: PASSED
