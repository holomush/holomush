---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 01
subsystem: api
tags: [grpc, abac, protobuf, connectrpc, privacy, ginkgo]

requires:
  - phase: 01-portal-spec
    provides: "01-SPEC §2.2/§2.3 audience-message split, §7.4/§7.5 profile shape, §8.4-§8.10 visibility model, §9.6.1 differential opacity assertion, §9.7 media shape"
  - phase: 02-abac-schema-vocabulary
    provides: "internal/access/profilevis conjunction evaluator, access.ViewerSubject / viewer-tier vocabulary, seed:profile-reachable, the viewer-flavored row-keyed twins, and 02-AUDIT-profile-public-read.sql (result sets 4 and 5 cover characters.description)"
provides:
  - "Proto package holomush.characteraccess.v1 — ProfileImage, PublicCharacter, GetCharacterProfile{Request,Response}, CharacterAccessService"
  - "WebService.WebGetCharacterProfile proxy pair and its internal/web handler"
  - "ABAC action token read_description plus seed:character-description-read and seed:viewer-character-description-read"
  - "world.CharacterDescription and world.Service.GetCharacterDescription — the projection-narrowing read"
  - "internal/grpc.CharacterAccessServer, its two narrow dependency interfaces, the D-83 viewer-identity seam, and projectPublic"
  - "(*integrationtest.Server).NewCharacterAccessServer harness constructor"
  - "test/integration/access/character_profile_read_test.go — six Ginkgo specs including the INV-PRIVACY-9 differential"
affects: [04-02, 04-04, 04-05, 04-06, 04-07, 04-09, phase-05-character-identity-ui]

actuals:
  tokens: 69360
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Per-audience proto message with structural absence (no player id / no location id field)"
    - "Narrow consumer-side dependency interfaces whose ABSENCES are the enforcement mechanism (compile fence, D-79)"
    - "Facade-owned viewer-identity resolution: session token -> rung, distinct from the scene facade's guest-denying gate (D-83)"
    - "Uniform opaque not-found: one message literal shared by every non-resolving branch"
    - "Paired-positive-control policy corpus with an append direction (profileCorpusStore)"

key-files:
  created:
    - api/proto/holomush/characteraccess/v1/characteraccess.proto
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_projection.go
    - internal/grpc/characteraccess_viewer_test.go
    - internal/web/character_handlers.go
    - test/integration/access/character_profile_read_test.go
  modified:
    - api/proto/holomush/web/v1/web.proto
    - internal/access/policy/seed.go
    - internal/access/policy/seed_test.go
    - internal/access/policy/seed_profile_visibility_test.go
    - internal/world/character.go
    - internal/world/service.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/sub_grpc.go
    - cmd/holomush/gateway.go
    - cmd/holomush/deps.go
    - internal/testsupport/integrationtest/harness.go

key-decisions:
  - "D-29's deferral is discharged by a NARROW ACTION (read_description), not by the deferred `permit(character, read, character)` shape. The action reaches exactly one method whose return type has no player-id or location-id field, so the CharacterInfo leak D-29 named is unreachable by construction."
  - "A read_description policy DENIAL collapses into the same uniform not-found as an absent row, rather than surfacing PermissionDenied — a distinguishable denial would disclose that the character exists. Unreachable in the shipped corpus; it is the fail-closed floor under a game that raises the description's floor."
  - "mapDescriptionError tests world.ErrNotFound BEFORE world.ErrAccessEvaluationFailed. An absent character carries both sentinels because the ABAC gate resolves character attributes before the row is read; testing the evaluation sentinel first produced Internal-vs-NotFound, a real id-existence oracle the differential spec caught."
  - "The gateway client leg (grpcclient + GRPCClient interface + gateway.go option) was added so the tracer is genuinely end to end; without it WebGetCharacterProfile returns Unimplemented in production."
  - "The Phase-2 D-29 guard test was extended rather than deleted: both new entries are pinned by compiled target shape AND exact DSL, and the deferred NAME seed:profile-public-read-character is still asserted absent."

patterns-established:
  - "profileCorpusStore: a control-corpus store that both EXCLUDES by name (counting removals so a disarmed control fails loudly) and APPENDS a policy — the append direction enables a targeted forbid on one resource instance, which is what makes a differential opacity assertion non-trivial."
  - "Rung-differential proof: swap the reachability permit for a guest-and-player-only variant, then drive the same request with and without a token. Both legs differ only in whether the facade resolved a higher rung, so the assertion cannot pass if every caller collapses to anonymous."

requirements-completed: [PROFILE-04, PROFILE-05, EXT-06]

coverage:
  - id: D1
    description: "A logged-out, off-location web visitor reads a character's name and in-world description through the CharacterAccessService facade"
    requirement: "PROFILE-05"
    verification:
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P1 returns the character's name and description with no session and no location"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P1b the name is the stored bytes verbatim"
        status: pass
    human_judgment: false
  - id: D2
    description: "The new viewer description permit is load-bearing — the same read against a corpus without seed:viewer-character-description-read does not carry the description"
    requirement: "PROFILE-04"
    verification:
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P2 paired positive control"
        status: pass
    human_judgment: false
  - id: D3
    description: "An unreachable profile is indistinguishable from a nonexistent character across status, message and marshaled body (INV-PRIVACY-9)"
    requirement: "PROFILE-05"
    verification:
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P3 the below-floor read and the no-such-row read are identical"
        status: pass
    human_judgment: false
  - id: D4
    description: "A character with zero profile.* rows still resolves a profile carrying its name; v0.13 emits zero images"
    requirement: "EXT-06"
    verification:
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P4, #P5"
        status: pass
    human_judgment: false
  - id: D5
    description: "The public path resolves its own viewer rung from the session token — three distinguishable rungs, anonymous degradation, and Internal on an identity outage (D-83)"
    requirement: "PROFILE-04"
    verification:
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P6 a guest token reaches a profile the anonymous rung cannot"
        status: pass
      - kind: unit
        ref: "task test -- -run 'ViewerIdentity|ViewerTier' ./internal/grpc/ (13 tests)"
        status: pass
    human_judgment: false
  - id: D6
    description: "A direct property-repository call from the facade does not compile (criterion 5, D-79)"
    verification:
      - kind: other
        ref: "recorded compiler diagnostic — see '## Criterion 5: the compile fence' below"
        status: pass
    human_judgment: false
  - id: D7
    description: "Criterion 6's exposure-audit obligation discharged by citation to Phase 2's committed audit; issue #4937 records it and stays open"
    verification:
      - kind: manual_procedural
        ref: "https://github.com/holomush/holomush/issues/4937#issuecomment-5254555327"
        status: pass
    human_judgment: false

duration: 35min
completed: 2026-08-11
status: complete
---

# Phase 04 Plan 01: End-to-end anonymous profile read Summary

**A logged-out visitor standing nowhere now reads a character's name and in-world description through `WebGetCharacterProfile`, and a profile below its reachability floor is byte-identical on the wire to a character that does not exist.**

## Performance

- **Duration:** 35 min
- **Started:** 2026-08-11T13:59:39Z
- **Completed:** 2026-08-11T14:35:16Z
- **Tasks:** 3 completed
- **Files modified:** 28 (18 hand-written, 10 generated)

## Accomplishments

- **The tracer runs end to end through every layer the phase touches** — ABAC seed policy, `world.Service`, the new `CharacterAccessService` facade, the `Web*` proxy, production wiring, the gateway client, and the integration harness — with a real Ginkgo verification, not a stubbed one.
- **D-29's deferral is discharged with a narrow action rather than the deferred shape.** `read_description` reaches exactly one method, `world.Service.GetCharacterDescription`, whose return type `world.CharacterDescription` has only `Name` and `Description`. The `CharacterInfo` projection carrying `PlayerId`/`LocationId` — the leak D-29 named — is unreachable by construction, not by a handler clearing fields.
- **The differential opacity spec caught a real information-disclosure bug during execution** (see Issues Encountered). An absent character id returned `Internal` while a below-floor profile returned `NotFound` — an oracle for which character ids exist. This is exactly the failure the tracer exists to surface after one commit rather than after ten.
- **D-83 is implemented literally:** the facade holds the session repositories and resolves all three rungs itself, including the GUEST rung that `playerGate.resolveAndGate` refuses outright (INV-SCENE-64).

## Task Commits

1. **Task 1 (tracer, TDD): End-to-end anonymous profile read** — four commits, in the dependency order the plan prescribed (proto first as the recovery point):
   - `1c776e6d5` (feat) — `characteraccess` proto package, the `Web*` proxy pair, the web handler, and all regenerated artifacts
   - `761298420` (feat) — the two `read_description` seed permits and `world.Service.GetCharacterDescription`
   - `97409387a` (feat) — the facade, projection, wiring, harness constructor, unit tests and the Ginkgo tracer spec
   - `2d6f38836` (docs) — reworded the `PublicCharacter` absence comment so the acceptance grep stays honest
2. **Task 2: Demonstrate and record the criterion-5 compile fence RED** — no commit. The fence itself landed in `97409387a`; Task 2's deliverable is the recorded diagnostic below, and its temporary call was reverted before any commit (`git status internal/grpc/` clean, `task build` green).
3. **Task 3: Discharge the exposure-audit obligation by citation** — no commit. The deliverable is a comment on GitHub issue #4937 plus the citation recorded below; the plan explicitly forbids creating a Phase-4 audit artifact.

**Plan metadata:** see the final `docs(04-01)` commit.

## RED demonstrated (TDD)

The Ginkgo spec was written before any production code and run to observe RED:

```
=== FAIL: test/integration/access  (0.00s)
FAIL	github.com/holomush/holomush/test/integration/access [setup failed]
no required module provides package github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1
```

After the surface existed it went RED again on a real assertion — the differential in P3 — and only then GREEN. That second RED is the one that mattered; see Issues Encountered.

## Criterion 5: the compile fence

Criterion 5's enforcement is the type system, not a lint rule and not a meta-test (D-79). A compile error cannot be asserted by a passing test, so the RED was demonstrated and is recorded here verbatim.

**The two interface names that constitute the fence** (`internal/grpc/characteraccess_service.go`):

- `characterAccessWorldReader` — declares exactly `ListPropertiesByParent` and `GetCharacterDescription`
- `characterAccessProfileVisibility` — declares exactly `Reachable` and `VisibleAttributes`

**The demonstration.** A temporary `_, _ = s.world.ListByParent(ctx, "character", characterID)` was added to `GetCharacterProfile` and `task build` run. Verbatim output:

```
task: [build] go build -tags realweb -o holomush ./cmd/holomush
# github.com/holomush/holomush/internal/grpc
internal/grpc/characteraccess_service.go:239:17: s.world.ListByParent undefined (type characterAccessWorldReader has no field or method ListByParent)
task: Failed to run task "build": exit status 1
```

The temporary statement was then reverted and `task build` confirmed green on the committed tree. No `gorules` entry and no `test/meta` AST test was added alongside it (`rg -n 'characteraccess' gorules/ .golangci.yaml` returns no match), per 01-SPEC §2.6 and D-79.

**Diagnostic note (LOW-1):** `rg -n 'ListByParent' internal/grpc/` returns one hit — the doc comment on `characterAccessWorldReader` naming what is deliberately absent. Per the plan this is a prompt to read the line, not a failure; the type fence is intact.

## Criterion 6: the exposure audit, discharged by citation (D-77)

No Phase-4 audit artifact was authored. The audit already exists and is committed:

- **`.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` result set (4)**, "Character in-world descriptions", at **lines 148-160** — `total_characters`, `nonempty_descriptions`, `longest_description_chars` over `characters`.
- **Result set (5)**, "Descriptions on guest-provisioned characters", at **lines 163-176** — the guest share of set (4).

Both read `characters.description` directly. The query is read-only by construction, emits no player-authored text, and its measured result is recorded in `02-AUDIT-RESULT.md` (three characters, **zero** with a non-empty description) with an explicit statement of the corpus limit.

Issue **#4937** carries a comment recording the Phase-4 widening against that evidence, naming both seed policy ids and stating that the populated-corpus re-run remains outstanding. The issue is **OPEN**, with labels unchanged.

## Files Created/Modified

**Created**

- `api/proto/holomush/characteraccess/v1/characteraccess.proto` — the facade contract: `ProfileImage`, `PublicCharacter` (no owning-player or location field), `GetCharacterProfile{Request,Response}`, `CharacterAccessService`
- `internal/grpc/characteraccess_service.go` — the facade, its two narrow dependency interfaces, `viewerIdentity`, `resolveViewerIdentity`, `resolveViewerTier`, `mapProfileError`, `mapDescriptionError`
- `internal/grpc/characteraccess_projection.go` — `projectPublic`, the sole constructor of `PublicCharacter`
- `internal/grpc/characteraccess_viewer_test.go` — 13 unit tests over the viewer-identity seam (behaviors 7-9)
- `internal/web/character_handlers.go` — `Handler.WebGetCharacterProfile`; the token comes from the header, never the body
- `test/integration/access/character_profile_read_test.go` — six Ginkgo specs, including the `// Verifies: INV-PRIVACY-9` differential

**Modified**

- `api/proto/holomush/web/v1/web.proto` — `WebGetCharacterProfile` RPC and its two messages
- `internal/access/policy/seed.go` — the two `read_description` permits and their rationale block, extending the trailing D-29 comment
- `internal/access/policy/seed_test.go`, `seed_profile_visibility_test.go` — census counts and the D-29 guard extended (see Deviations)
- `internal/world/character.go`, `internal/world/service.go` — `CharacterDescription` and `GetCharacterDescription`
- `internal/web/handler.go` — `CharacterAccessClient` interface, field and `WithCharacterAccessClient` option
- `internal/grpcclient/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/gateway.go` — the gateway client leg
- `cmd/holomush/sub_grpc.go` — facade construction and registration beside the scene facade
- `internal/testsupport/integrationtest/harness.go` — `NewCharacterAccessServer`
- Generated: `pkg/proto/holomush/{characteraccess,web}/v1/**`, `web/src/lib/connect/holomush/{characteraccess,web}/v1/**`, `site/src/content/docs/reference/grpc-api.md`

## Decisions Made

1. **A `read_description` policy denial collapses into the uniform not-found rather than surfacing `PermissionDenied`.** The plan's behavior 2 says the control corpus "returns the description absent"; a distinguishable "you may not read this description" would disclose that the character exists, which is the class of leak §8.7 closes. Since the description is a field of the profile rather than a separate resource, a denial there has the same meaning to a viewer as an unreachable profile. In the shipped v0.13 corpus the branch is unreachable — the viewer twin permits every rung unconditionally — so it is the fail-closed floor under a future game that raises the description's floor (§7.4), not a live behavior. Documented in `mapDescriptionError`'s doc comment.
2. **The gateway client leg was added.** The plan's step 6 names only `sub_grpc.go` and the harness, but without `grpcclient.GetCharacterProfile`, the `GRPCClient` interface method and the `gateway.go` option, `WebGetCharacterProfile` returns `Unimplemented` in production and the tracer is not end to end. Rule 2.
3. **Name and description travel together through one gated read.** The must-have "name is emitted structurally, gated only by reachability" is satisfied because `read_description` is not the per-attribute floor evaluation (`profilevis` terms A and B over property rows) — name comes from the character row, and the shipped viewer twin permits every rung, so reachability is name's only effective gate. A future plan that introduces a per-attribute floor for the description will need to split the two reads.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The Phase-2 D-29 guard test and three seed census tests turn RED on any new seed**

- **Found during:** Task 1 (ABAC step)
- **Issue:** `TestNoPhase2SeedIntroducesACharacterResourceTypePermit`, `TestSeedPoliciesCount`, `TestSeedPoliciesExpectedNames` and `TestSeedPoliciesEffectDistribution` pin the seed corpus by set equality. Adding the two permits fails all four. The guard test's own doc comment names Phase 4 as the phase that discharges it.
- **Fix:** Extended rather than relaxed. Both entries were added to the name set and to `wantShapes` (which pins compiled principal and action list), and each gained an **exact-DSL assertion** plus a `SeedVersion` pin — so widening either action list to `read` turns the guard RED even though the name set would be unchanged. The deferred name `seed:profile-public-read-character` is still asserted absent, with the reason restated: the deferred SHAPE must not be resurrected under cover of "Phase 4 shipped it". Counts updated to 63 seeds / 53 permits with the provenance note the file's convention requires.
- **Files modified:** `internal/access/policy/seed_test.go`, `internal/access/policy/seed_profile_visibility_test.go`
- **Verification:** `task test -- ./internal/access/...` green (2474 tests)
- **Committed in:** `761298420`

**2. [Rule 3 - Blocking] `GRPCClient` interface widening breaks every gateway test mock**

- **Found during:** Task 1 (wiring step)
- **Issue:** Adding `GetCharacterProfile` to `cmd/holomush.GRPCClient` broke `mockGRPCClient` at eleven call sites.
- **Fix:** Added the one method to the mock, matching the sibling `GetPublishedScene` stub form.
- **Files modified:** `cmd/holomush/deps_test.go`
- **Verification:** `task test -- ./cmd/... ./internal/...` green (9882 tests)
- **Committed in:** `97409387a`

**3. [Rule 1 - Bug] `player_id` / `location_id` acceptance grep turned red on the doc comment that explains their absence**

- **Found during:** Task 1 (final criteria check)
- **Issue:** `rg -n 'player_id|location_id' api/proto/.../characteraccess.proto` returned 2 matches — both inside the `PublicCharacter` doc comment describing which fields are deliberately absent. This is the identical failure mode the plan itself records for the `resolveAndGate` and `ListByParent` greps: the criterion goes red exactly when the plan is followed correctly.
- **Fix:** Reworded the comment to describe the absent fields without spelling the snake_case tokens, and added a sentence saying why, so a future editor does not reintroduce them. The grep can now only fail on a field that was ADDED. Derived artifacts regenerated.
- **Files modified:** `api/proto/holomush/characteraccess/v1/characteraccess.proto` (+ regenerated `.pb.go`, `_pb.ts`, `grpc-api.md`)
- **Verification:** criterion returns 0 matches; `task lint:proto` green
- **Committed in:** `2d6f38836`

---

**Total deviations:** 3 auto-fixed (2 × Rule 3, 1 × Rule 1)
**Impact on plan:** All three were required to keep the tree green or to make an acceptance criterion honest. No scope creep — the guard-test edit is the discharge the guard's own doc comment anticipated, and the grep fix removes a false-red rather than weakening a gate.

## Issues Encountered

**The differential opacity spec found a real information-disclosure bug, and it was not in the code the plan told me to write.**

P3 drives a below-floor profile and a nonexistent character id through the same RPC. It failed:

```
[FAILED] Expected
    <codes.Code>: 5
to equal
    <codes.Code>: 13
```

The below-floor read returned `NotFound` (5) as designed; the nonexistent id returned `Internal` (13). The cause sits one layer down: `world.Service.checkAccess` evaluates `read_description` against `character:<id>` **before** the row is read, and `attribute.CharacterProvider.Resolve` fetches the character to build the attribute bag. For a nonexistent id that fetch fails, the engine reports an evaluation failure, and `checkAccess` classifies it as `ErrAccessEvaluationFailed` — which `mapDescriptionError` was correctly (per §8.10) rendering as `Internal`.

The result was a working oracle for which character ids exist, reachable by an anonymous caller, with both individual behaviors defensible in isolation.

**Fix:** `mapDescriptionError` now tests `world.ErrNotFound` **first**. The absent-character error carries both sentinels — the provider wraps the repository's `world.ErrNotFound` and `checkAccess` joins `ErrAccessEvaluationFailed` on top — so sentinel ORDER decides the outcome. A not-found reached through the attribute resolver is still a not-found; §8.10's rule is about outages, and a missing row is not one. A genuine engine outage carries no `ErrNotFound` and still returns `Internal`. The reasoning is recorded in the function's doc comment so the order is not "cleaned up" later.

This is the tracer earning its keep: the bug is in the seam between two layers, it would have been inherited by every later plan expanding horizontally from this slice, and no single-layer test would have seen it.

## Known Stubs

None. The `profile` map, `primary_image` and `gallery` are empty by design in this slice — v0.13 mints no media identifier (01-SPEC §9.7), and the viewer-filtered property slice arrives in plan 04-02, which is stated in `GetCharacterProfile`'s body comment and in the proto field comments. `VisibleAttributes` is declared on the narrow visibility interface and is not yet called; that is the seam 04-02 wires, not a stub returning fake data.

## Threat Flags

None. Every surface this plan added is inside the plan's own threat register: the new RPC and its `Web*` proxy are T-04-01/T-04-10, the seeded permits are T-04-07, and the token-handling path is T-04-27/T-04-28. No new endpoint, auth path, file-access pattern or schema change beyond those.

## Notes for the Phase Gate

- `/holomush-dev:review-abac` is REQUIRED before push for this phase — two ABAC seed policies were added and a Phase-2 D-29 guard test was amended.
- `docs/architecture/invariants.yaml` is untouched by this plan, as instructed. INV-PRIVACY-9 stays `binding: pending`; the `// Verifies:` annotation is in place above the differential spec so plan 04-05 Task 3 can flip it to `bound`.

## Self-Check: PASSED

All six created files verified present on disk; all four commit hashes verified present in `git log --oneline --all`.

Gates re-run on the committed tree: `task build` (0), `task lint` (0), `task test` (0), `task lint:proto` (green), `task test:int -- ./test/integration/access/...` (green — 72 specs run, 71 pre-existing plus this file's six, 1 skipped).
